package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"network-switch-visualizer/internal/auth"
	"network-switch-visualizer/internal/db"
	"network-switch-visualizer/internal/models"
	"network-switch-visualizer/internal/snmp"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// --- Rate Limiting ---

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors = make(map[string]*visitor)
	mu       sync.Mutex
)

func getVisitor(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	v, exists := visitors[ip]
	if !exists {
		limiter := rate.NewLimiter(0.2, 5) // 1 request every 5 seconds (0.2 rps), burst of 5
		visitors[ip] = &visitor{limiter, time.Now()}
		return limiter
	}

	v.lastSeen = time.Now()
	return v.limiter
}

func RateLimiterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		limiter := getVisitor(ip)
		if !limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// --- Middleware ---

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("token")
		if err != nil {
			if err == http.ErrNoCookie {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		tknStr := c.Value
		claims, err := auth.ParseToken(tknStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Context could be set here if needed
		_ = claims
		next(w, r)
	}
}

func AdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("token")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tknStr := c.Value
		claims, err := auth.ParseToken(tknStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// --- Audit Logging ---

func logAuditWithReq(r *http.Request, event, user, details string) {
	ip := r.RemoteAddr
	log.Printf("[AUDIT] Event=%s User=%s Details=%s IP=%s", event, user, details, ip)
}

// --- Handlers ---

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	var creds models.Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	u, err := db.GetUser(creds.Username)
	if err != nil {
		logAuditWithReq(r, "LOGIN_FAILED", creds.Username, "User not found")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(creds.Password)); err != nil {
		logAuditWithReq(r, "LOGIN_FAILED", creds.Username, "Invalid password")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	expirationTime := time.Now().Add(30 * time.Minute)
	claims := &models.Claims{
		Username: creds.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(auth.JwtKey)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	secureCookie := os.Getenv("COOKIE_SECURE") == "true"
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    tokenString,
		Expires:  expirationTime,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteStrictMode,
	})

	logAuditWithReq(r, "LOGIN_SUCCESS", creds.Username, "Role="+u.Role)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                   "ok",
		"role":                     u.Role,
		"username":                 creds.Username,
		"password_change_required": !u.PasswordChanged,
	})
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("token")
	user := "unknown"
	if c != nil {
		if claims, err := auth.ParseToken(c.Value); err == nil {
			user = claims.Username
		}
	}
	logAuditWithReq(r, "LOGOUT", user, "")

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusOK)
}

func HandleMe(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("token")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	claims, err := auth.ParseToken(c.Value)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	u, err := db.GetUser(claims.Username)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"username":                 claims.Username,
		"role":                     u.Role,
		"password_change_required": !u.PasswordChanged,
	})
}

func HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, err := r.Cookie("token")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	claims, err := auth.ParseToken(c.Value)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	type ChangePasswordReq struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	var req ChangePasswordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	u, err := db.GetUser(claims.Username)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}
	if err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.OldPassword)); err != nil {
		logAuditWithReq(r, "PASSWORD_CHANGE_FAILED", claims.Username, "Invalid old password")
		http.Error(w, "Invalid old password", http.StatusUnauthorized)
		return
	}

	if err := db.UpdateUserPassword(claims.Username, req.NewPassword); err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	logAuditWithReq(r, "PASSWORD_CHANGE_SUCCESS", claims.Username, "")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Password changed successfully"})
}

func HandleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		users, err := db.GetAllUsers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(users)
	case "POST":
		type NewUserReq struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		var req NewUserReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := auth.ValidatePassword(req.Password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Role == "" {
			req.Role = "user"
		}
		if err := db.CreateUser(req.Username, req.Password, req.Role); err != nil {
			logAuditWithReq(r, "USER_CREATE_FAILED", "admin", fmt.Sprintf("TargetUser=%s Error=%v", req.Username, err))
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		logAuditWithReq(r, "USER_CREATE_SUCCESS", "admin", fmt.Sprintf("TargetUser=%s Role=%s", req.Username, req.Role))
		w.WriteHeader(http.StatusCreated)
	case "DELETE":
		idStr := r.URL.Query().Get("id")
		if err := db.DeleteUser(idStr); err != nil {
			logAuditWithReq(r, "USER_DELETE_FAILED", "admin", fmt.Sprintf("TargetID=%s Error=%v", idStr, err))
			http.Error(w, "Failed to delete user", http.StatusInternalServerError)
			return
		}
		logAuditWithReq(r, "USER_DELETE_SUCCESS", "admin", fmt.Sprintf("TargetID=%s", idStr))
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func HandleSwitchesWithRoleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleSwitches(w, r)
		return
	}
	c, _ := r.Cookie("token")
	claims, _ := auth.ParseToken(c.Value)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	handleSwitches(w, r)
}

func handleSwitches(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		switches, err := db.GetAllSwitches()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(switches)
	case "POST":
		var s models.Switch
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.Community == "" {
			s.Community = "public"
		}
		s.Enabled = true

		if s.Name == "" {
			sysName, err := snmp.GetSysName(r.Context(), s.IPAddress, s.Community)
			if err != nil {
				if r.Context().Err() != nil {
					return
				}
				s.Name = s.IPAddress
			} else {
				s.Name = sysName
			}
		}

		newConfig, detectedPorts, err := snmp.GenerateConfigFromSNMP(r.Context(), s)

		if r.Context().Err() != nil {
			return
		}

		if err == nil {
			s.Config = newConfig
			s.DetectedPorts = detectedPorts
		} else {
			s.Config = generateDefaultConfig()
			s.DetectedPorts = 0
		}

		id, err := db.CreateSwitch(s)
		if err != nil {
			log.Printf("Error creating device: %v", err)
			http.Error(w, "Failed to create device", http.StatusInternalServerError)
			return
		}
		s.ID = id
		json.NewEncoder(w).Encode(s)
	case "DELETE":
		idStr := r.URL.Query().Get("id")
		if err := db.DeleteSwitch(idStr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "PUT":
		var s models.Switch
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := db.UpdateSwitch(s); err != nil {
			log.Printf("Error updating device: %v", err)
			http.Error(w, "Failed to update device", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(s)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func HandleSwitchSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)
	s, err := db.GetSwitch(id)
	if err != nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	newConfig, detectedPorts, err := snmp.GenerateConfigFromSNMP(r.Context(), s)
	if err != nil {
		http.Error(w, fmt.Sprintf("Sync failed: %v", err), http.StatusInternalServerError)
		return
	}
	if err := db.UpdateSwitchConfig(s.ID, newConfig, detectedPorts); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// Update the struct to return the fresh data
	s.Config = newConfig
	s.DetectedPorts = detectedPorts

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(s)
}

func HandleSwitchStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)
	s, err := db.GetSwitch(id)
	if err != nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	if !s.Enabled {
		sysInfo := models.SystemInfo{
			Name:     s.Name,
			Descr:    "Monitoring Disabled",
			UpTime:   "-",
			Contact:  "-",
			Location: "-",
		}
		mappedSections := mapConfigToView(s.Config, map[string]models.SNMPResult{}, s.AllowPortZero)
		response := models.SwitchStatusResponse{System: sysInfo, Sections: mappedSections}
		json.NewEncoder(w).Encode(response)
		return
	}

	snmpData, sysInfo, err := snmp.PollSNMP(r.Context(), s)
	if err != nil {
		log.Printf("SNMP Error for device %s (%s): %v. Using mock data.", s.Name, s.IPAddress, err)
		json.NewEncoder(w).Encode(generateMockView(s.Config))
		return
	}
	mappedSections := mapConfigToView(s.Config, snmpData, s.AllowPortZero)
	response := models.SwitchStatusResponse{System: sysInfo, Sections: mappedSections}
	json.NewEncoder(w).Encode(response)
}

// --- Helpers ---

func generateDefaultConfig() models.SwitchConfig {
	return models.SwitchConfig{Sections: []models.PortSection{{ID: "sec-1", Title: "Default", PortType: "RJ45", Layout: "odd_top", Rows: 2, PortRanges: "1-48"}}}
}

func parsePortRanges(rangeStr string) []int {
	var ports []int
	parts := strings.Split(rangeStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
				if err1 == nil && err2 == nil && start <= end {
					for i := start; i <= end; i++ {
						ports = append(ports, i)
					}
				}
			}
		} else {
			if idx, err := strconv.Atoi(part); err == nil {
				ports = append(ports, idx)
			}
		}
	}
	sort.Ints(ports)
	return ports
}

func createPortStatus(phyIdx int, pType string, res models.SNMPResult, isChild bool) models.PortStatus {
	statusStr := "DOWN"
	if res.OperStatus == 1 {
		statusStr = "UP"
	}
	if isChild {
		if strings.Contains(pType, "QSFP28") {
			pType = "SFP28"
		} else if strings.Contains(pType, "QSFP+") {
			pType = "SFP+"
		}
	}
	return models.PortStatus{
		PhysicalIndex: phyIdx, PortType: pType, Status: statusStr, IfName: res.IfName, IfDesc: res.IfAlias,
		Speed: res.HighSpeed * 1000000, InTraffic: res.InOctets, OutTraffic: res.OutOctets,
		InRate: res.InRate, OutRate: res.OutRate,
		VlanID: res.VlanID, AllowedVlans: res.AllowedVlans, Mode: res.Mode,
		DOM: res.DOM,
	}
}

func mapConfigToView(config models.SwitchConfig, snmpData map[string]models.SNMPResult, allowPortZero bool) []models.PortSection {
	var views []models.PortSection
	snmpByPhyIdx := make(map[int][]models.SNMPResult)

	// Helper to get physical index

	for _, res := range snmpData {
		if snmp.IsIgnoredInterface(res.IfName) {
			continue
		}
		phyIdx, ok := snmp.GetPhysicalIndex(res.IfName, res.IfAlias)
		if ok {
			if phyIdx == 0 && !allowPortZero {
				continue
			}
			snmpByPhyIdx[phyIdx] = append(snmpByPhyIdx[phyIdx], res)
		}
	}

	for _, sec := range config.Sections {
		indices := parsePortRanges(sec.PortRanges)
		var portStatuses []models.PortStatus
		for _, idx := range indices {
			if idx == 0 && !allowPortZero {
				continue
			}
			results, found := snmpByPhyIdx[idx]
			if !found || len(results) == 0 {
				portStatuses = append(portStatuses, models.PortStatus{
					PhysicalIndex: idx, PortType: sec.PortType, Status: "DOWN", IfName: fmt.Sprintf("Port %d", idx),
				})
				continue
			}
			isBreakout := len(results) > 1

			if isBreakout {
				sort.Slice(results, func(i, j int) bool { return results[i].IfName < results[j].IfName })
				parent := models.PortStatus{
					PhysicalIndex: idx, PortType: sec.PortType, Status: "UP", IsBreakout: true, IfName: fmt.Sprintf("Port %d (Breakout)", idx),
				}
				for _, subRes := range results {
					parent.BreakoutPorts = append(parent.BreakoutPorts, createPortStatus(idx, sec.PortType, subRes, true))
				}
				portStatuses = append(portStatuses, parent)
			} else {
				portStatuses = append(portStatuses, createPortStatus(idx, sec.PortType, results[0], false))
			}
		}
		viewSec := sec
		var portsIf []interface{}
		for _, p := range portStatuses {
			portsIf = append(portsIf, p)
		}
		viewSec.Ports = portsIf
		views = append(views, viewSec)
	}
	return views
}

func getMockDOM() models.DOMInfo {
	temp := 45.5
	volt := 3.3
	tx := -2.5
	rx := -5.1
	return models.DOMInfo{Temperature: &temp, Voltage: &volt, TxPower: &tx, RxPower: &rx}
}

func generateMockView(config models.SwitchConfig) models.SwitchStatusResponse {
	var sections []models.PortSection
	for _, sec := range config.Sections {
		mockSec := sec
		portIndices := parsePortRanges(sec.PortRanges)
		var mockPorts []models.PortStatus
		for _, idx := range portIndices {
			isUp := idx%3 != 0
			status := "DOWN"
			if isUp {
				status = "UP"
			}
			mode := "access"
			if idx > 20 {
				mode = "trunk"
			}
			ps := models.PortStatus{
				PhysicalIndex: idx, PortType: sec.PortType, Status: status,
				IfName: fmt.Sprintf("Eth%d", idx), IfDesc: "Mock Interface", Speed: 10000000000,
				InRate: uint64(idx * 500000), OutRate: uint64(idx * 120000), VlanID: 1, Mode: mode, DOM: getMockDOM(),
			}
			mockPorts = append(mockPorts, ps)
		}
		var portsAsInterface []interface{}
		for _, p := range mockPorts {
			portsAsInterface = append(portsAsInterface, p)
		}
		mockSec.Ports = portsAsInterface
		sections = append(sections, mockSec)
	}
	return models.SwitchStatusResponse{
		System:   models.SystemInfo{Name: "Mock-Device", Descr: "Mock Device", UpTime: "10 days", Contact: "admin", Location: "Lab"},
		Sections: sections,
	}
}
