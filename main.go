package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"
)

// TerraformOutput is used to unmarshal the JSON output of terraform command
type TerraformOutput struct {
	InstancePublicIP struct {
		Value string `json:"value"`
	} `json:"instance_public_ip"`
}

// PlistData holds the data to be filled in the plist template
type PlistData struct {
	Label   string
	IP      string
	Args    string
	Path    string
	Cwd     string
	LogPath string
}

// PlistTemplate is the boilerplate for the .plist file
const PlistTemplate = `
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{.Label}}</string>

  <key>ProgramArguments</key>
  <array>
    <string>{{.Args}}</string>
  </array>

  <key>EnvironmentVariables</key>
<dict>
  <key>PATH</key>
  <string>/usr/local/bin:{{.Path}}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin:</string>
</dict>

  <key>StartInterval</key>
  <integer>600</integer>

  <key>StandardOutPath</key>
  <string>{{.LogPath}}</string>

  <key>StandardErrorPath</key>
  <string>{{.LogPath}}</string>

  <key>WorkingDirectory</key>
  <string>{{.Cwd}}</string>

  <key>RunAtLoad</key>
  <true/>
</dict>
</plist>
`

func main() {
	// Remove timestamp from log output
	log.SetFlags(0)

	// Define and parse the ip, launchd, label and cwd flags
	ipPtr := flag.String("ip", "", "IP address to use instead of running Terraform")
	launchdPtr := flag.Bool("launchd", false, "Create launchd .plist file")
	labelPtr := flag.String("label", "com.mytarsnap", "The label for the .plist file")
	cwdPtr := flag.String("cwd", ".", "Working directory for the launchd task")
	flag.Parse()

	// Expand cwd into an absolute path
	absCwd, err := filepath.Abs(*cwdPtr)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	var ip string

	// Check if the ip flag was set
	if *ipPtr != "" {
		log.Println("Using provided IP address...")
		ip = *ipPtr
	} else {
		log.Println("Running Terraform command to get output...")

		out, err := exec.Command("terraform", "-chdir=./terraform", "output", "-json").Output()
		if err != nil {
			log.Fatalf("Failed to execute command: %v", err)
		}

		log.Println("Parsing JSON output...")

		var tfOutput TerraformOutput
		err = json.Unmarshal(out, &tfOutput)
		if err != nil {
			log.Fatalf("Failed to parse JSON: %v", err)
		}

		ip = tfOutput.InstancePublicIP.Value
	}

	// Create a launchd .plist file if the launchd flag is set
	if *launchdPtr {
		log.Println("Creating launchd .plist file...")

		tmpl, err := template.New("plist").Parse(PlistTemplate)
		if err != nil {
			log.Fatalf("Failed to create template: %v", err)
		}

		exePath, err := os.Executable()
		if err != nil {
			panic(err)
		}

		exeName := filepath.Base(exePath)
		fmt.Println(exeName)

		// Here's where we change the filename and label logic
		filename := fmt.Sprintf("%s.%s.plist", *labelPtr, ip)

		data := PlistData{
			Label:   filename[:len(filename)-6], // trim .plist extension
			IP:      ip,
			Args:    exePath,
			Path:    os.Args[0],
			Cwd:     absCwd,
			LogPath: fmt.Sprintf("/tmp/%s.log", os.Args[0]),
		}

		file, err := os.Create(filename)
		if err != nil {
			log.Fatalf("Failed to create .plist file: %v", err)
		}
		defer file.Close()

		err = tmpl.Execute(file, data)
		if err != nil {
			log.Fatalf("Failed to execute template: %v", err)
		}

		log.Println("Successfully created launchd .plist file.")
		return
	}

	log.Println("Copying remote bash history file to local machine...")

	// Set SSH user
	user := "root"

	// Create local directory if it does not exist
	localDir := "./data/bash_history"
	err = os.MkdirAll(localDir, 0o755)
	if err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}

	// Append current timestamp to the filename
	localFile := fmt.Sprintf("%s/bash_history_%s.txt", localDir, time.Now().Format("20060102_150405"))
	absLocalFile, err := filepath.Abs(localFile)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Execute the scp command to copy the remote bash history file to local machine
	cmd := exec.Command("scp", fmt.Sprintf("%s@%s:~/.bash_history", user, ip), absLocalFile)
	log.Println("Executing command:", cmd.String()) // Logging the command
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	log.Println("Successfully copied remote bash history file to local machine.")

	log.Println("Output from the scp command:")
	log.Println(string(out))

	log.Println("Finished.")
}
