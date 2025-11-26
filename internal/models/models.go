package models

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type SectionPort struct {
	PhysicalIndex int    `json:"physical_index"`
	PortType      string `json:"port_type"`
	DefaultIfName string `json:"default_if_name"`
}

type PortSection struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Type       string        `json:"type"`
	PortType   string        `json:"port_type"`
	Layout     string        `json:"layout"`
	LayoutType string        `json:"layout_type"`
	Rows       int           `json:"rows"`
	PortRanges string        `json:"port_ranges"`
	IsCombo    bool          `json:"is_combo"`
	Ports      []interface{} `json:"ports"`
}

type SwitchConfig struct {
	Sections []PortSection `json:"sections"`
}

type Switch struct {
	ID                int          `json:"id"`
	Name              string       `json:"name"`
	IPAddress         string       `json:"ip_address"`
	Community         string       `json:"community"`
	DetectedPorts     int          `json:"detected_ports"`
	AllowPortZero     bool         `json:"allow_port_zero"`
	Enabled           bool         `json:"enabled"`
	SectionConfigJSON string       `json:"-"`
	Config            SwitchConfig `json:"config"`
}

type DOMInfo struct {
	Temperature *float64 `json:"temperature"`
	Voltage     *float64 `json:"voltage"`
	TxPower     *float64 `json:"tx_power"`
	RxPower     *float64 `json:"rx_power"`
	BiasCurrent *float64 `json:"bias_current"`
}

type PortStatus struct {
	PhysicalIndex int          `json:"physical_index"`
	PortType      string       `json:"port_type"`
	Status        string       `json:"status"`
	IfName        string       `json:"if_name"`
	IfDesc        string       `json:"if_desc"`
	Speed         uint64       `json:"speed"`
	InTraffic     uint64       `json:"in_traffic"`
	OutTraffic    uint64       `json:"out_traffic"`
	InRate        uint64       `json:"in_rate"`
	OutRate       uint64       `json:"out_rate"`
	VlanID        int          `json:"vlan_id"`
	AllowedVlans  string       `json:"allowed_vlans"`
	Mode          string       `json:"mode"`
	IsBreakout    bool         `json:"is_breakout"`
	BreakoutPorts []PortStatus `json:"breakout_ports,omitempty"`
	DOM           DOMInfo      `json:"dom"`
}

type SystemInfo struct {
	Name     string `json:"name"`
	Descr    string `json:"descr"`
	UpTime   string `json:"uptime"`
	Contact  string `json:"contact"`
	Location string `json:"location"`
}

type SwitchStatusResponse struct {
	System   SystemInfo    `json:"system"`
	Sections []PortSection `json:"sections"`
}

type SNMPResult struct {
	IfIndex      int
	IfName       string
	IfAlias      string
	OperStatus   int
	HighSpeed    uint64
	InOctets     uint64
	OutOctets    uint64
	InRate       uint64
	OutRate      uint64
	VlanID       int
	AllowedVlans string
	Mode         string
	DOM          DOMInfo
}

type SensorData struct {
	Index int
	Descr string
	Type  int
	Scale int
	Value int64
}

type IfInfo struct {
	Name      string
	HighSpeed uint64
}

type User struct {
	ID              int       `json:"id"`
	Username        string    `json:"username"`
	PasswordHash    string    `json:"-"`
	Role            string    `json:"role"`
	PasswordChanged bool      `json:"password_changed"`
	CreatedAt       time.Time `json:"created_at"`
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}
