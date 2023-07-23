package main

import (
	"bufio"
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

// TerraformOutput is used to unmarshal the JSON output of the terraform command
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
  <integer>3600</integer>

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

// Generate data/bash_history/summary.txt that contains the unique list of bash lines
func generateSummaryFile(logDir string) {
	// Get the unique list of bash lines
	uniqueLines := getUniqueBashLines(logDir)

	// Create or open the summary.txt file
	summaryFile, err := os.Create(filepath.Join(logDir, "summary.txt"))
	if err != nil {
		log.Fatalf("Failed to create summary.txt: %v", err)
	}
	defer summaryFile.Close()

	// Write the unique lines to the summary file
	for _, line := range uniqueLines {
		_, err := fmt.Fprintln(summaryFile, line)
		if err != nil {
			log.Fatalf("Failed to write to summary.txt: %v", err)
		}
	}

	log.Println("Successfully generated summary.txt.")
}

// readLines reads all lines from a file and returns the line count and slice of lines
func readLines(filename string) (int, []string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return 0, nil, err
	}

	return len(lines), lines, nil
}

// getUniqueLineCount returns the count of unique lines in a slice
func getUniqueLineCount(lines []string) int {
	uniqueLines := make(map[string]struct{})
	for _, line := range lines {
		uniqueLines[line] = struct{}{}
	}
	return len(uniqueLines)
}

// getUniqueBashLines returns the unique list of bash lines from all data files
func getUniqueBashLines(logDir string) []string {
	// Map to store the unique lines
	uniqueLines := make(map[string]struct{})

	// Walk through the files in the directory
	filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatalf("Failed to walk through files: %v", err)
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Open the file
		file, err := os.Open(path)
		if err != nil {
			log.Fatalf("Failed to open file: %v", err)
		}
		defer file.Close()

		// Scan the lines and add unique lines to the map
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			uniqueLines[line] = struct{}{}
		}

		if err := scanner.Err(); err != nil {
			log.Fatalf("Failed to scan file: %v", err)
		}

		return nil
	})

	// Convert the map keys to a slice of strings and return
	var lines []string
	for line := range uniqueLines {
		lines = append(lines, line)
	}

	return lines
}

func main() {
	// Define and parse the ip, launchd, label, and cwd flags
	ipPtr := flag.String("ip", "", "IP address to use instead of running Terraform")
	launchdPtr := flag.Bool("launchd", false, "Create launchd .plist file")
	labelPtr := flag.String("label", "com.tarsnap", "The label for the .plist file")
	cwdPtr := flag.String("cwd", ".", "Working directory for the launchd task")
	showFullPtr := flag.Bool("show-full", false, "Show the unique list of lines to stdout")
	flag.Parse()

	var absCwd string

	// If --show-full flag is provided, only show the unique list of bash lines
	if *showFullPtr {
		logDir := "./data/bash_history"
		uniqueLines := getUniqueBashLines(logDir)
		for _, line := range uniqueLines {
			fmt.Println(line)
		}
		return
	}

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

		absExePath, err := filepath.Abs(exePath)
		if err != nil {
			panic(err)
		}

		exeDir := filepath.Dir(absExePath)
		fmt.Println("Executable Path:", absExePath)
		fmt.Println("Executable Directory:", exeDir)

		exeName := filepath.Base(exePath)
		fmt.Println(exeName)

		// Here's where we change the filename and label logic
		filename := fmt.Sprintf("%s.%s.plist", *labelPtr, ip)

		data := PlistData{
			Label:   filename[:len(filename)-6], // trim .plist extension
			IP:      ip,
			Args:    absExePath,
			Path:    exeDir,
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

	log.Println("Copying remote bash history file to the local machine...")

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

	// Execute the scp command to copy the remote bash history file to the local machine
	cmd := exec.Command("scp", fmt.Sprintf("%s@%s:~/.bash_history", user, ip), absLocalFile)
	log.Println("Executing command:", cmd.String()) // Logging the command
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	log.Println("Successfully copied remote bash history file to the local machine.")

	log.Println("Output from the scp command:")
	log.Println(string(out))

	// Loop over all the files in the data/bash_history directory
	log.Println("Summary of data files:")
	lineCounts := make(map[string]int)
	aggregateLines := []string{}

	err = filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Only consider regular files
			fileLines, lines, err := readLines(path)
			if err != nil {
				return err
			}
			lineCounts[path] = fileLines
			aggregateLines = append(aggregateLines, lines...)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Failed to walk through files: %v", err)
	}

	// Display the summary of data files
	for path, count := range lineCounts {
		log.Printf("File: %s, Line Count: %d", path, count)
	}

	// Get the unique line count for the aggregate of all files
	uniqueLineCount := getUniqueLineCount(aggregateLines)
	log.Printf("Unique Line Count for Aggregate of All Files: %d", uniqueLineCount)

	log.Println("Finished.")

	// Generate summary.txt file containing unique list of bash lines
	generateSummaryFile(localDir)
}
