package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	serviceName    = "DapodikProxy"
	serviceDisplay = "Dapodik Proxy Server"
	serviceDesc    = "Proxy server untuk Dapodik Web Service API"
	firewallRule   = "DapodikProxy-8888"
	defaultPort    = "8888"
)

// Config struct to match proxy config
type Config struct {
	Server struct {
		Port         int      `yaml:"port"`
		ReadTimeout  int      `yaml:"read_timeout"`
		WriteTimeout int      `yaml:"write_timeout"`
		AllowedIPs   []string `yaml:"allowed_ips"`
	} `yaml:"server"`
	Dapodik struct {
		BaseURL string `yaml:"base_url"`
	} `yaml:"dapodik"`
	Logging struct {
		Level  string `yaml:"level"`
		Pretty bool   `yaml:"pretty"`
	} `yaml:"logging"`
	Security struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"security"`
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           DAPODIK PROXY INSTALLER                            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Check if running as administrator
	if !isAdmin() {
		fmt.Println("❌ ERROR: Installer must be run as Administrator")
		fmt.Println()
		fmt.Println("Please:")
		fmt.Println("  1. Right-click on installer.exe")
		fmt.Println("  2. Select 'Run as Administrator'")
		fmt.Println()
		pause()
		os.Exit(1)
	}

	// Get executable directory
	exePath, err := os.Executable()
	if err != nil {
		fatal("Failed to get executable path: %v", err)
	}
	installDir := filepath.Dir(exePath)

	// Check if proxy executable exists
	proxyExe := filepath.Join(installDir, "dapodik-proxy-windows-amd64.exe")
	if _, err := os.Stat(proxyExe); os.IsNotExist(err) {
		fatal("Proxy executable not found: %s", proxyExe)
	}

	// Check if config exists
	configFile := filepath.Join(installDir, "config.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fatal("Config file not found: %s", configFile)
	}

	fmt.Println("Installation Details:")
	fmt.Printf("  Service Name    : %s\n", serviceName)
	fmt.Printf("  Install Dir     : %s\n", installDir)
	fmt.Printf("  Executable      : %s\n", proxyExe)
	fmt.Printf("  Config          : %s\n", configFile)
	fmt.Println()

	// Show menu
	fmt.Println("Please select an option:")
	fmt.Println("  [1] Install Service")
	fmt.Println("  [2] Uninstall Service")
	fmt.Println("  [3] Start Service")
	fmt.Println("  [4] Stop Service")
	fmt.Println("  [5] Service Status")
	fmt.Println("  [0] Exit")
	fmt.Println()
	fmt.Print("Enter your choice: ")

	var choice string
	fmt.Scanln(&choice)
	fmt.Println()

	switch choice {
	case "1":
		installService(proxyExe, configFile, installDir)
	case "2":
		uninstallService()
	case "3":
		startService()
	case "4":
		stopService()
	case "5":
		serviceStatus()
	case "0":
		fmt.Println("Exiting...")
		os.Exit(0)
	default:
		fatal("Invalid choice")
	}

	pause()
}

func installService(exePath, configPath, workDir string) {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("INSTALLING SERVICE")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Check if service already exists
	if serviceExists() {
		fmt.Println("⚠️  Service already exists. Stopping and removing old service...")
		stopService()
		removeService()
		fmt.Println()
	}

	// Handle API Key generation
	apiKey, err := ensureAPIKey(configPath)
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to handle API Key: %v\n", err)
	}

	// Create service using sc.exe

	fmt.Println("→ Creating Windows service...")

	binPath := fmt.Sprintf("\"%s\" -config \"%s\"", exePath, configPath)

	cmd := exec.Command("sc.exe", "create", serviceName,
		"binPath=", binPath,
		"start=", "auto",
		"DisplayName=", serviceDisplay,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		fatal("Failed to create service: %v\n%s", err, string(output))
	}

	// Set service description
	cmd = exec.Command("sc.exe", "description", serviceName, serviceDesc)
	cmd.Run() // Ignore error, description is optional

	fmt.Println("✓ Service created successfully")

	// Configure firewall
	fmt.Println()
	fmt.Println("→ Configuring Windows Firewall...")

	// Remove existing rule if any
	exec.Command("netsh", "advfirewall", "firewall", "delete", "rule",
		"name="+firewallRule).Run()

	// Add firewall rule
	cmd = exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+firewallRule,
		"dir=in",
		"action=allow",
		"protocol=TCP",
		"localport="+defaultPort,
		"enable=yes",
		"profile=any",
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("⚠️  Warning: Failed to configure firewall: %v\n%s\n", err, string(output))
		fmt.Println("   You may need to manually open port", defaultPort)
	} else {
		fmt.Printf("✓ Firewall rule added (Port %s)\n", defaultPort)
	}

	// Start service
	fmt.Println()
	fmt.Println("→ Starting service...")

	cmd = exec.Command("sc.exe", "start", serviceName)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("⚠️  Warning: Failed to start service: %v\n%s\n", err, string(output))
		fmt.Println("   You can start it manually later")
	} else {
		fmt.Println("✓ Service started successfully")
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("✅ INSTALLATION COMPLETE!")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println()

	if apiKey != "" {
		fmt.Println("🔐 SECURITY NOTICE:")
		fmt.Printf("   API Key generated: %s\n", apiKey)
		fmt.Println("   This key has been saved to config.yaml and access_token.txt")
		fmt.Println("   Keep this key secret! Use it in the 'X-API-Key' header from your main app.")
		fmt.Println()

		// Save to access_token.txt for easy copy-paste
		tokenFilePath := filepath.Join(filepath.Dir(configPath), "access_token.txt")
		os.WriteFile(tokenFilePath, []byte(apiKey), 0644)
	}

	fmt.Printf("Service '%s' has been installed and started.\n", serviceName)
	fmt.Println("It will start automatically on system boot.")
	fmt.Println()
	fmt.Printf("Health Check: http://localhost:%s/health\n", defaultPort)
	fmt.Println()
}

func ensureAPIKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", err
	}

	// If API Key already exists, don't change it
	if config.Security.APIKey != "" {
		return config.Security.APIKey, nil
	}

	// Generate new random API Key
	fmt.Println("→ Generating secure API Key...")
	newKey := generateRandomKey(32)
	config.Security.APIKey = newKey

	// Marshal back to YAML
	newData, err := yaml.Marshal(&config)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, newData, 0644); err != nil {
		return "", err
	}

	return newKey, nil
}

func generateRandomKey(length int) string {
	b := make([]byte, length/2)
	if _, err := rand.Read(b); err != nil {
		// Fallback to simpler random if crypto/rand fails
		return "proxy-secret-key-" + hex.EncodeToString([]byte(serviceName))[:8]
	}
	return hex.EncodeToString(b)
}

func uninstallService() {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("UNINSTALLING SERVICE")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	if !serviceExists() {
		fmt.Println("⚠️  Service is not installed")
		return
	}

	// Stop service
	fmt.Println("→ Stopping service...")
	stopService()

	// Remove service
	fmt.Println()
	fmt.Println("→ Removing service...")
	removeService()

	// Remove firewall rule
	fmt.Println()
	fmt.Println("→ Removing firewall rule...")
	cmd := exec.Command("netsh", "advfirewall", "firewall", "delete", "rule",
		"name="+firewallRule)

	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("⚠️  Warning: Failed to remove firewall rule: %v\n%s\n", err, string(output))
	} else {
		fmt.Println("✓ Firewall rule removed")
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("✅ UNINSTALLATION COMPLETE!")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println()
}

func startService() {
	fmt.Println("→ Starting service...")
	cmd := exec.Command("sc.exe", "start", serviceName)

	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "already") {
			fmt.Println("ℹ️  Service is already running")
		} else {
			fatal("Failed to start service: %v\n%s", err, string(output))
		}
	} else {
		fmt.Println("✓ Service started successfully")
	}
}

func stopService() {
	fmt.Println("→ Stopping service...")
	cmd := exec.Command("sc.exe", "stop", serviceName)

	if output, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(output), "not started") {
			fmt.Println("ℹ️  Service is not running")
		} else {
			fmt.Printf("⚠️  Warning: Failed to stop service: %v\n%s\n", err, string(output))
		}
	} else {
		fmt.Println("✓ Service stopped successfully")
	}
}

func removeService() {
	cmd := exec.Command("sc.exe", "delete", serviceName)

	if output, err := cmd.CombinedOutput(); err != nil {
		fatal("Failed to delete service: %v\n%s", err, string(output))
	}

	fmt.Println("✓ Service removed successfully")
}

func serviceStatus() {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("SERVICE STATUS")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	cmd := exec.Command("sc.exe", "query", serviceName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		fmt.Println("❌ Service is NOT installed")
	} else {
		fmt.Println(string(output))
	}
}

func serviceExists() bool {
	cmd := exec.Command("sc.exe", "query", serviceName)
	err := cmd.Run()
	return err == nil
}

func isAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("\n❌ ERROR: "+format+"\n\n", args...)
	pause()
	os.Exit(1)
}

func pause() {
	fmt.Print("Press Enter to exit...")
	fmt.Scanln()
}
