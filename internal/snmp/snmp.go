package snmp

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"network-switch-visualizer/internal/models"

	"github.com/gosnmp/gosnmp"
)

const (
	oidSysDescr                     = ".1.3.6.1.2.1.1.1.0"
	oidSysUpTime                    = ".1.3.6.1.2.1.1.3.0"
	oidSysContact                   = ".1.3.6.1.2.1.1.4.0"
	oidSysName                      = ".1.3.6.1.2.1.1.5.0"
	oidSysLocation                  = ".1.3.6.1.2.1.1.6.0"
	oidIfName                       = ".1.3.6.1.2.1.31.1.1.1.1"
	oidIfAlias                      = ".1.3.6.1.2.1.31.1.1.1.18"
	oidIfOperStatus                 = ".1.3.6.1.2.1.2.2.1.8"
	oidIfHighSpeed                  = ".1.3.6.1.2.1.31.1.1.1.15"
	oidIfHCInOctets                 = ".1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOutOctets                = ".1.3.6.1.2.1.31.1.1.1.10"
	oidDot1dBasePortIfIndex         = ".1.3.6.1.2.1.17.1.4.1.2"
	oidDot1qPvid                    = ".1.3.6.1.2.1.17.7.1.4.5.1.1"
	oidDot1qVlanStaticEgressPorts   = ".1.3.6.1.2.1.17.7.1.4.3.1.2"
	oidDot1qVlanStaticUntaggedPorts = ".1.3.6.1.2.1.17.7.1.4.3.1.4"
	oidVmVlan                       = ".1.3.6.1.4.1.9.9.68.1.2.2.1.2"
	oidVlanTrunkPortNativeVlan      = ".1.3.6.1.4.1.9.9.46.1.6.1.1.5"
	oidVlanTrunkPortDynamicStatus   = ".1.3.6.1.4.1.9.9.46.1.6.1.1.14"

	oidVlanTrunkPortVlansEnabled   = ".1.3.6.1.4.1.9.9.46.1.6.1.1.4"
	oidVlanTrunkPortVlansXEnabled  = ".1.3.6.1.4.1.9.9.46.1.6.1.1.17"
	oidVlanTrunkPortVlans2kEnabled = ".1.3.6.1.4.1.9.9.46.1.6.1.1.18"
	oidVlanTrunkPortVlans3kEnabled = ".1.3.6.1.4.1.9.9.46.1.6.1.1.19"

	oidEntPhysicalDescr = ".1.3.6.1.2.1.47.1.1.1.1.2"
	oidEntSensorType    = ".1.3.6.1.2.1.99.1.1.1.1"
	oidEntSensorScale   = ".1.3.6.1.2.1.99.1.1.1.2"
	oidEntSensorValue   = ".1.3.6.1.2.1.99.1.1.1.4"
)

var (
	reColonSub      = regexp.MustCompile(`\/(\d+):\d+$`)
	reLastNum       = regexp.MustCompile(`(\d+)$`)
	reSonicBreakout = regexp.MustCompile(`(?i)Eth\s*(\d+)\s*/\s*(\d+)`)
	reSonicSimple   = regexp.MustCompile(`(?i)Eth\s*(\d+)$`)
)

type TrafficCacheEntry struct {
	Timestamp time.Time
	InOctets  uint64
	OutOctets uint64
}

var (
	trafficCache = make(map[string]*TrafficCacheEntry)
	cacheMutex   sync.Mutex
)

func createSNMPParams(s models.Switch) *gosnmp.GoSNMP {
	return &gosnmp.GoSNMP{
		Target:    s.IPAddress,
		Port:      161,
		Community: s.Community,
		Version:   gosnmp.Version2c,
		Timeout:   2 * time.Second,
		Retries:   1,
	}
}

func GetSysName(ctx context.Context, ip, community string) (string, error) {
	type result struct {
		name string
		err  error
	}
	resChan := make(chan result, 1)

	go func() {
		params := &gosnmp.GoSNMP{
			Target: ip, Port: 161, Community: community, Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1,
		}
		if err := params.Connect(); err != nil {
			resChan <- result{err: err}
			return
		}
		defer params.Conn.Close()
		pkt, err := params.Get([]string{oidSysName})
		if err != nil {
			resChan <- result{err: err}
			return
		}
		for _, variable := range pkt.Variables {
			if variable.Name == oidSysName {
				if val, ok := variable.Value.([]byte); ok {
					resChan <- result{name: string(val)}
					return
				}
				if val, ok := variable.Value.(string); ok {
					resChan <- result{name: val}
					return
				}
			}
		}
		resChan <- result{err: fmt.Errorf("sysName OID not found")}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resChan:
		return res.name, res.err
	}
}

func PollSNMP(ctx context.Context, s models.Switch) (map[string]models.SNMPResult, models.SystemInfo, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex // Protects shared maps

	sysInfo := models.SystemInfo{}
	rawResults := make(map[int]*models.SNMPResult)

	getResult := func(idx int) *models.SNMPResult {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := rawResults[idx]; !ok {
			rawResults[idx] = &models.SNMPResult{IfIndex: idx}
		}
		return rawResults[idx]
	}

	errChan := make(chan error, 10)
	doneChan := make(chan struct{})

	checkCtx := func() bool {
		select {
		case <-ctx.Done():
			return true
		default:
			return false
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if checkCtx() {
			return
		}
		params := createSNMPParams(s)
		if err := params.Connect(); err != nil {
			errChan <- err
			return
		}
		defer params.Conn.Close()

		pkt, err := params.Get([]string{oidSysName, oidSysDescr, oidSysContact, oidSysLocation, oidSysUpTime})
		if err == nil {
			for _, v := range pkt.Variables {
				valStr := ""
				switch v.Type {
				case gosnmp.OctetString:
					valStr = string(v.Value.([]byte))
				case gosnmp.TimeTicks:
					ticks := gosnmp.ToBigInt(v.Value).Uint64()
					duration := time.Duration(ticks) * 10 * time.Millisecond
					valStr = duration.String()
				}
				name := v.Name
				if strings.HasSuffix(name, oidSysName[1:]) {
					sysInfo.Name = valStr
				}
				if strings.HasSuffix(name, oidSysDescr[1:]) {
					sysInfo.Descr = valStr
				}
				if strings.HasSuffix(name, oidSysContact[1:]) {
					sysInfo.Contact = valStr
				}
				if strings.HasSuffix(name, oidSysLocation[1:]) {
					sysInfo.Location = valStr
				}
				if strings.HasSuffix(name, oidSysUpTime[1:]) {
					sysInfo.UpTime = valStr
				}
			}
		}
	}()

	ifOids := []string{oidIfName, oidIfAlias, oidIfOperStatus, oidIfHighSpeed, oidIfHCInOctets, oidIfHCOutOctets}
	for _, oid := range ifOids {
		wg.Add(1)
		go func(o string) {
			defer wg.Done()
			if checkCtx() {
				return
			}
			params := createSNMPParams(s)
			if err := params.Connect(); err != nil {
				errChan <- err
				return
			}
			defer params.Conn.Close()

			_ = params.BulkWalk(o, func(pdu gosnmp.SnmpPDU) error {
				if checkCtx() {
					return fmt.Errorf("cancelled")
				}
				idx := extractIndex(pdu.Name, o)
				if idx > 0 {
					switch o {
					case oidIfName:
						getResult(idx).IfName = string(pdu.Value.([]byte))
					case oidIfAlias:
						getResult(idx).IfAlias = string(pdu.Value.([]byte))
					case oidIfOperStatus:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getResult(idx).OperStatus = int(val.Int64())
						}
					case oidIfHighSpeed:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getResult(idx).HighSpeed = val.Uint64()
						}
					case oidIfHCInOctets:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getResult(idx).InOctets = val.Uint64()
						}
					case oidIfHCOutOctets:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getResult(idx).OutOctets = val.Uint64()
						}
					}
				}
				return nil
			})
		}(oid)
	}

	accessVlanMap := make(map[int]int)
	trunkNativeMap := make(map[int]int)
	trunkAllowedVlans := make(map[int][]int)
	trunkStatusMap := make(map[int]int)
	basePortToIfIndex := make(map[int]int)
	pvidMap := make(map[int]int)
	qBridgeVlanInfo := make(map[int]struct {
		NativeVlan   int
		AllowedVlans []int
	})

	var vlanMu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		if checkCtx() {
			return
		}
		params := createSNMPParams(s)
		if err := params.Connect(); err != nil {
			errChan <- err
			return
		}
		defer params.Conn.Close()

		_ = params.BulkWalk(oidVmVlan, func(pdu gosnmp.SnmpPDU) error {
			if checkCtx() {
				return fmt.Errorf("cancelled")
			}
			idx := extractIndex(pdu.Name, oidVmVlan)
			val := gosnmp.ToBigInt(pdu.Value)
			if idx > 0 && val != nil {
				vlanMu.Lock()
				accessVlanMap[idx] = int(val.Int64())
				vlanMu.Unlock()
			}
			return nil
		})

		_ = params.BulkWalk(oidVlanTrunkPortNativeVlan, func(pdu gosnmp.SnmpPDU) error {
			if checkCtx() {
				return fmt.Errorf("cancelled")
			}
			idx := extractIndex(pdu.Name, oidVlanTrunkPortNativeVlan)
			val := gosnmp.ToBigInt(pdu.Value)
			if idx > 0 && val != nil {
				vlanMu.Lock()
				trunkNativeMap[idx] = int(val.Int64())
				vlanMu.Unlock()
			}
			return nil
		})

		walkVlanBitmap := func(oid string, offset int) {
			_ = params.BulkWalk(oid, func(pdu gosnmp.SnmpPDU) error {
				if checkCtx() {
					return fmt.Errorf("cancelled")
				}
				idx := extractIndex(pdu.Name, oid)
				if idx > 0 {
					if bytes, ok := pdu.Value.([]byte); ok {
						vlanMu.Lock()
						trunkAllowedVlans[idx] = append(trunkAllowedVlans[idx], parseVlanBitmap(bytes, offset)...)
						vlanMu.Unlock()
					}
				}
				return nil
			})
		}
		walkVlanBitmap(oidVlanTrunkPortVlansEnabled, 0)
		walkVlanBitmap(oidVlanTrunkPortVlansXEnabled, 1024)
		walkVlanBitmap(oidVlanTrunkPortVlans2kEnabled, 2048)
		walkVlanBitmap(oidVlanTrunkPortVlans3kEnabled, 3072)

		_ = params.BulkWalk(oidVlanTrunkPortDynamicStatus, func(pdu gosnmp.SnmpPDU) error {
			if checkCtx() {
				return fmt.Errorf("cancelled")
			}
			idx := extractIndex(pdu.Name, oidVlanTrunkPortDynamicStatus)
			val := gosnmp.ToBigInt(pdu.Value)
			if idx > 0 && val != nil {
				vlanMu.Lock()
				trunkStatusMap[idx] = int(val.Int64())
				vlanMu.Unlock()
			}
			return nil
		})

		_ = params.BulkWalk(oidDot1dBasePortIfIndex, func(pdu gosnmp.SnmpPDU) error {
			if checkCtx() {
				return fmt.Errorf("cancelled")
			}
			basePort := extractIndex(pdu.Name, oidDot1dBasePortIfIndex)
			val := gosnmp.ToBigInt(pdu.Value)
			if basePort > 0 && val != nil {
				vlanMu.Lock()
				basePortToIfIndex[basePort] = int(val.Int64())
				vlanMu.Unlock()
			}
			return nil
		})
	}()

	domMap := make(map[int]models.DOMInfo)
	sensorRawMap := make(map[int]*models.SensorData)
	var domMu sync.Mutex

	getSensor := func(idx int) *models.SensorData {
		domMu.Lock()
		defer domMu.Unlock()
		if _, ok := sensorRawMap[idx]; !ok {
			sensorRawMap[idx] = &models.SensorData{Index: idx}
		}
		return sensorRawMap[idx]
	}

	domOids := []string{oidEntPhysicalDescr, oidEntSensorType, oidEntSensorScale, oidEntSensorValue}
	for _, oid := range domOids {
		wg.Add(1)
		go func(o string) {
			defer wg.Done()
			if checkCtx() {
				return
			}
			params := createSNMPParams(s)
			if err := params.Connect(); err != nil {
				errChan <- err
				return
			}
			defer params.Conn.Close()

			_ = params.BulkWalk(o, func(pdu gosnmp.SnmpPDU) error {
				if checkCtx() {
					return fmt.Errorf("cancelled")
				}
				idx := extractIndex(pdu.Name, o)
				if idx > 0 {
					switch o {
					case oidEntPhysicalDescr:
						getSensor(idx).Descr = string(pdu.Value.([]byte))
					case oidEntSensorType:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getSensor(idx).Type = int(val.Int64())
						}
					case oidEntSensorScale:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getSensor(idx).Scale = int(val.Int64())
						}
					case oidEntSensorValue:
						val := gosnmp.ToBigInt(pdu.Value)
						if val != nil {
							getSensor(idx).Value = val.Int64()
						}
					}
				}
				return nil
			})
		}(oid)
	}

	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-ctx.Done():
		return nil, models.SystemInfo{}, ctx.Err()
	case <-doneChan:
		close(errChan)
	}

	if len(errChan) > 0 {
		if len(rawResults) == 0 {
			return nil, sysInfo, <-errChan
		}
	}

	if len(basePortToIfIndex) > 0 {
		params := createSNMPParams(s)
		if err := params.Connect(); err == nil {
			defer params.Conn.Close()
			_ = params.BulkWalk(oidDot1qPvid, func(pdu gosnmp.SnmpPDU) error {
				basePort := extractIndex(pdu.Name, oidDot1qPvid)
				val := gosnmp.ToBigInt(pdu.Value)
				if basePort > 0 && val != nil {
					if ifIndex, exists := basePortToIfIndex[basePort]; exists {
						pvidMap[ifIndex] = int(val.Int64())
					}
				}
				return nil
			})

			_ = params.BulkWalk(oidDot1qVlanStaticEgressPorts, func(pdu gosnmp.SnmpPDU) error {
				vlanID := extractIndex(pdu.Name, oidDot1qVlanStaticEgressPorts)
				if vlanID > 0 {
					if bytes, ok := pdu.Value.([]byte); ok {
						ports := parsePortBitmap(bytes)
						for _, basePort := range ports {
							if ifIndex, ok := basePortToIfIndex[basePort]; ok {
								info := qBridgeVlanInfo[ifIndex]
								info.AllowedVlans = append(info.AllowedVlans, vlanID)
								qBridgeVlanInfo[ifIndex] = info
							}
						}
					}
				}
				return nil
			})

			_ = params.BulkWalk(oidDot1qVlanStaticUntaggedPorts, func(pdu gosnmp.SnmpPDU) error {
				vlanID := extractIndex(pdu.Name, oidDot1qVlanStaticUntaggedPorts)
				if vlanID > 0 {
					if bytes, ok := pdu.Value.([]byte); ok {
						ports := parsePortBitmap(bytes)
						for _, basePort := range ports {
							if ifIndex, ok := basePortToIfIndex[basePort]; ok {
								info := qBridgeVlanInfo[ifIndex]
								info.NativeVlan = vlanID
								qBridgeVlanInfo[ifIndex] = info
							}
						}
					}
				}
				return nil
			})
		}
	}

	for _, sData := range sensorRawMap {
		if sData.Descr == "" || sData.Type == 0 {
			continue
		}
		phyIdx, ok := GetPhysicalIndex(sData.Descr, "")
		if !ok {
			continue
		}
		dom, ok := domMap[phyIdx]
		if !ok {
			dom = models.DOMInfo{}
		}
		val := float64(sData.Value) * getScaleMultiplier(sData.Scale)
		floatPtr := func(v float64) *float64 { return &v }
		switch sData.Type {
		case 8:
			dom.Temperature = floatPtr(val)
		case 9:
			dom.Voltage = floatPtr(val)
		case 10:
			dom.BiasCurrent = floatPtr(val * 1000.0)
		case 11:
			if val > 0 {
				dbm := 10 * math.Log10(val*1000.0)
				descrLower := strings.ToLower(sData.Descr)
				if strings.Contains(descrLower, "tx") || strings.Contains(descrLower, "output") {
					dom.TxPower = floatPtr(dbm)
				}
				if strings.Contains(descrLower, "rx") || strings.Contains(descrLower, "input") {
					dom.RxPower = floatPtr(dbm)
				}
			}
		}
		domMap[phyIdx] = dom
	}

	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	finalResults := make(map[string]models.SNMPResult)

	trunkAllowedMap := make(map[int]string)
	for idx, vlans := range trunkAllowedVlans {
		trunkAllowedMap[idx] = indicesToRangeString(vlans)
	}

	for idx, res := range rawResults {
		if res.IfName == "" {
			continue
		}
		cacheKey := fmt.Sprintf("%d-%d", s.ID, idx)
		var inRate, outRate uint64
		if prev, ok := trafficCache[cacheKey]; ok {
			duration := time.Since(prev.Timestamp).Seconds()
			if duration > 0 {
				var inDiff, outDiff uint64
				if res.InOctets >= prev.InOctets {
					inDiff = res.InOctets - prev.InOctets
				}
				if res.OutOctets >= prev.OutOctets {
					outDiff = res.OutOctets - prev.OutOctets
				}
				inRate = uint64(float64(inDiff*8) / duration)
				outRate = uint64(float64(outDiff*8) / duration)
			}
		}
		trafficCache[cacheKey] = &TrafficCacheEntry{Timestamp: time.Now(), InOctets: res.InOctets, OutOctets: res.OutOctets}
		res.InRate = inRate
		res.OutRate = outRate

		phyIdx, ok := GetPhysicalIndex(res.IfName, res.IfAlias)
		if ok {
			if dom, ok := domMap[phyIdx]; ok {
				res.DOM = dom
			}
		}

		isTrunk := false
		if status, ok := trunkStatusMap[res.IfIndex]; ok {
			if status == 1 {
				isTrunk = true
			}
		} else {
			if _, ok := trunkAllowedMap[res.IfIndex]; ok {
				isTrunk = true
			}
		}

		if isTrunk {
			res.Mode = "trunk"
			if v, ok := trunkNativeMap[res.IfIndex]; ok {
				res.VlanID = v
			}
			if list, ok := trunkAllowedMap[res.IfIndex]; ok {
				res.AllowedVlans = list
			}
		} else {
			res.Mode = "access"
			if v, ok := accessVlanMap[res.IfIndex]; ok {
				res.VlanID = v
			} else if v, ok := trunkNativeMap[res.IfIndex]; ok {
				res.VlanID = v
			} else if v, ok := pvidMap[res.IfIndex]; ok {
				res.VlanID = v
			}
		}

		if res.VlanID == 0 && res.Mode == "access" && len(res.AllowedVlans) == 0 {
			if info, ok := qBridgeVlanInfo[res.IfIndex]; ok {
				if len(info.AllowedVlans) > 0 {
					res.AllowedVlans = indicesToRangeString(info.AllowedVlans)
				}
				if info.NativeVlan > 0 {
					res.VlanID = info.NativeVlan
				}
			}
		}

		finalResults[fmt.Sprintf("%d", idx)] = *res
	}

	return finalResults, sysInfo, nil
}

func GenerateConfigFromSNMP(ctx context.Context, s models.Switch) (models.SwitchConfig, int, error) {
	snmpData, _, err := PollSNMP(ctx, s)
	if err != nil {
		return models.SwitchConfig{}, 0, err
	}
	var phyIndices []int
	for _, res := range snmpData {
		if IsIgnoredInterface(res.IfName) {
			continue
		}
		phyIdx, ok := GetPhysicalIndex(res.IfName, res.IfAlias)
		if ok {
			if phyIdx == 0 && !s.AllowPortZero {
				continue
			}
			phyIndices = append(phyIndices, phyIdx)
		}
	}
	if len(phyIndices) == 0 {
		return models.SwitchConfig{}, 0, fmt.Errorf("no valid ports found")
	}
	sort.Ints(phyIndices)
	var uniqueIndices []int
	if len(phyIndices) > 0 {
		uniqueIndices = append(uniqueIndices, phyIndices[0])
		for i := 1; i < len(phyIndices); i++ {
			if phyIndices[i] != phyIndices[i-1] {
				uniqueIndices = append(uniqueIndices, phyIndices[i])
			}
		}
	}
	maxPort := 0
	if len(uniqueIndices) > 0 {
		maxPort = uniqueIndices[len(uniqueIndices)-1]
	}

	rangeStr := indicesToRangeString(uniqueIndices)
	section := models.PortSection{
		ID: "sec-1", Title: "All Ports", Type: "RJ45", PortType: "RJ45", Layout: "odd_top", LayoutType: "odd_top", Rows: 2, PortRanges: rangeStr, Ports: nil,
	}
	return models.SwitchConfig{Sections: []models.PortSection{section}}, maxPort, nil
}

func GetPhysicalIndex(name, alias string) (int, bool) {
	// Reject subinterfaces (e.g., Gi0/0.100)
	if strings.Contains(name, ".") {
		return 0, false
	}

	// Prioritize explicit breakout naming (e.g. ethernet1/1/11:3)
	// This prevents alias hijacking where the description contains "Eth 1" etc.
	matches := reColonSub.FindStringSubmatch(name)
	if len(matches) > 1 {
		idx, _ := strconv.Atoi(matches[1])
		return idx, true
	}

	if alias != "" {
		matches = reSonicBreakout.FindStringSubmatch(alias)
		if len(matches) > 1 {
			idx, _ := strconv.Atoi(matches[1])
			return idx, true
		}
		matches = reSonicSimple.FindStringSubmatch(alias)
		if len(matches) > 1 {
			idx, _ := strconv.Atoi(matches[1])
			return idx, true
		}
	}
	matches = reSonicBreakout.FindStringSubmatch(name)
	if len(matches) > 1 {
		idx, _ := strconv.Atoi(matches[1])
		return idx, true
	}
	matches = reSonicSimple.FindStringSubmatch(name)
	if len(matches) > 1 {
		idx, _ := strconv.Atoi(matches[1])
		return idx, true
	}
	matches = reLastNum.FindStringSubmatch(name)
	if len(matches) > 1 {
		idx, _ := strconv.Atoi(matches[1])
		return idx, true
	}
	return 0, false
}

func IsIgnoredInterface(name string) bool {
	// Explicitly ignore subinterfaces
	if strings.Contains(name, ".") {
		return true
	}
	lower := strings.ToLower(name)
	if lower == "eth0" || strings.HasPrefix(lower, "tunnel") {
		return true
	}
	prefixes := []string{"vl", "nu", "lo", "po", "st", "mg", "ma", "in", "bl", "co", "tu", "bd", "vi", "cpu", "bridge-aggregation", "br", "ap", "us", "lan"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func extractIndex(oid string, rootOid string) int {
	if !strings.HasPrefix(oid, rootOid) {
		return -1
	}
	suffix := strings.TrimPrefix(oid, rootOid)
	suffix = strings.TrimPrefix(suffix, ".")
	if idx, err := strconv.Atoi(suffix); err == nil {
		return idx
	}
	return -1
}

func indicesToRangeString(indices []int) string {
	if len(indices) == 0 {
		return ""
	}
	sort.Ints(indices)
	var parts []string
	start := indices[0]
	prev := indices[0]
	for i := 1; i < len(indices); i++ {
		curr := indices[i]
		if curr == prev+1 {
			prev = curr
		} else {
			if start == prev {
				parts = append(parts, fmt.Sprintf("%d", start))
			} else {
				parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
			}
			start = curr
			prev = curr
		}
	}
	if start == prev {
		parts = append(parts, fmt.Sprintf("%d", start))
	} else {
		parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
	}
	return strings.Join(parts, ", ")
}

func parseVlanBitmap(bytes []byte, offset int) []int {
	var vlans []int
	for i, b := range bytes {
		for bit := 0; bit < 8; bit++ {
			if (b>>(7-bit))&1 == 1 {
				vlanID := offset + i*8 + bit
				if vlanID > 0 && vlanID < 4096 {
					vlans = append(vlans, vlanID)
				}
			}
		}
	}
	return vlans
}

func parsePortBitmap(bytes []byte) []int {
	var ports []int
	for i, b := range bytes {
		for bit := 0; bit < 8; bit++ {
			if (b>>(7-bit))&1 == 1 {
				portIdx := i*8 + bit + 1
				ports = append(ports, portIdx)
			}
		}
	}
	return ports
}

func getScaleMultiplier(scaleCode int) float64 {
	switch scaleCode {
	case 1:
		return 1e-24
	case 2:
		return 1e-21
	case 3:
		return 1e-18
	case 4:
		return 1e-15
	case 5:
		return 1e-12
	case 6:
		return 1e-9
	case 7:
		return 1e-6
	case 8:
		return 1e-3
	case 9:
		return 1.0
	case 10:
		return 1e3
	case 11:
		return 1e6
	case 12:
		return 1e9
	default:
		return 1.0
	}
}
