package db

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"

	"network-switch-visualizer/internal/models"

	_ "github.com/glebarez/go-sqlite"
	"golang.org/x/crypto/bcrypt"
)

var DB *sql.DB

func InitDB(dbPath string) error {
	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	initSQL, err := os.ReadFile("schema.sql")
	if err == nil {
		DB.Exec(string(initSQL))
	}

	return initAdminUser()
}

func initAdminUser() error {

	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='password_changed'").Scan(&count)
	if err == nil && count == 0 {
		log.Println("Migrating users table: adding password_changed column")
		_, err = DB.Exec("ALTER TABLE users ADD COLUMN password_changed BOOLEAN DEFAULT 0")
		if err != nil {
			log.Printf("Error adding password_changed column: %v", err)
		}
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('switches') WHERE name='detected_ports'").Scan(&count)
	if err == nil && count == 0 {
		log.Println("Migrating switches table: adding detected_ports column")
		_, err = DB.Exec("ALTER TABLE switches ADD COLUMN detected_ports INTEGER DEFAULT 0")
		if err != nil {
			log.Printf("Error adding detected_ports column: %v", err)
		}
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('switches') WHERE name='allow_port_zero'").Scan(&count)
	if err == nil && count == 0 {
		log.Println("Migrating switches table: adding allow_port_zero column")
		_, err = DB.Exec("ALTER TABLE switches ADD COLUMN allow_port_zero BOOLEAN DEFAULT 0")
		if err != nil {
			log.Printf("Error adding allow_port_zero column: %v", err)
		}
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('switches') WHERE name='enabled'").Scan(&count)
	if err == nil && count == 0 {
		log.Println("Migrating switches table: adding enabled column")
		_, err = DB.Exec("ALTER TABLE switches ADD COLUMN enabled BOOLEAN DEFAULT 1")
		if err != nil {
			log.Printf("Error adding enabled column: %v", err)
		}
	}

	err = DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		_, err := DB.Exec("INSERT INTO users (username, password_hash, role, password_changed) VALUES (?, ?, ?, ?)", "admin", string(hashedPassword), "admin", false)
		if err != nil {
			return err
		}
		log.Println("Default admin user created (admin/admin)")
	}
	return nil
}

func GetUser(username string) (models.User, error) {
	var u models.User
	err := DB.QueryRow("SELECT id, username, password_hash, role, password_changed FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.PasswordChanged)
	return u, err
}

func CreateUser(username, password, role string) error {
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	_, err := DB.Exec("INSERT INTO users (username, password_hash, role, password_changed) VALUES (?, ?, ?, ?)", username, string(hashedPassword), role, false)
	return err
}

func GetAllUsers() ([]models.User, error) {
	rows, err := DB.Query("SELECT id, username, role, password_changed, created_at FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		rows.Scan(&u.ID, &u.Username, &u.Role, &u.PasswordChanged, &u.CreatedAt)
		users = append(users, u)
	}
	return users, nil
}

func DeleteUser(id string) error {
	_, err := DB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func UpdateUserPassword(username, newPassword string) error {
	newHash, _ := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	_, err := DB.Exec("UPDATE users SET password_hash = ?, password_changed = 1 WHERE username = ?", string(newHash), username)
	return err
}

func GetSwitch(id int) (models.Switch, error) {
	var s models.Switch
	err := DB.QueryRow("SELECT id, name, ip_address, community, detected_ports, allow_port_zero, enabled, section_config FROM switches WHERE id = ?", id).
		Scan(&s.ID, &s.Name, &s.IPAddress, &s.Community, &s.DetectedPorts, &s.AllowPortZero, &s.Enabled, &s.SectionConfigJSON)
	if err != nil {
		return s, err
	}
	json.Unmarshal([]byte(s.SectionConfigJSON), &s.Config)
	return s, nil
}

func GetAllSwitches() ([]models.Switch, error) {
	rows, err := DB.Query("SELECT id, name, ip_address, community, detected_ports, allow_port_zero, enabled, section_config FROM switches")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var switches []models.Switch
	for rows.Next() {
		var s models.Switch
		rows.Scan(&s.ID, &s.Name, &s.IPAddress, &s.Community, &s.DetectedPorts, &s.AllowPortZero, &s.Enabled, &s.SectionConfigJSON)
		json.Unmarshal([]byte(s.SectionConfigJSON), &s.Config)
		switches = append(switches, s)
	}
	return switches, nil
}

func CreateSwitch(s models.Switch) (int, error) {
	defaultJSON, _ := json.Marshal(s.Config)

	res, err := DB.Exec("INSERT INTO switches (name, ip_address, community, detected_ports, allow_port_zero, enabled, section_config) VALUES (?, ?, ?, ?, ?, ?, ?)",
		s.Name, s.IPAddress, s.Community, s.DetectedPorts, s.AllowPortZero, s.Enabled, string(defaultJSON))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func UpdateSwitchConfig(id int, config models.SwitchConfig, detectedPorts int) error {
	configJSON, _ := json.Marshal(config)
	_, err := DB.Exec("UPDATE switches SET section_config = ?, detected_ports = ? WHERE id = ?", string(configJSON), detectedPorts, id)
	return err
}

func UpdateSwitch(s models.Switch) error {
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return err
	}
	_, err = DB.Exec("UPDATE switches SET name = ?, ip_address = ?, community = ?, detected_ports = ?, allow_port_zero = ?, enabled = ?, section_config = ? WHERE id = ?",
		s.Name, s.IPAddress, s.Community, s.DetectedPorts, s.AllowPortZero, s.Enabled, string(configJSON), s.ID)
	return err
}

func DeleteSwitch(id string) error {
	_, err := DB.Exec("DELETE FROM switches WHERE id = ?", id)
	return err
}
