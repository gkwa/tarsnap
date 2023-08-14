package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"inet.af/netaddr"
)

// TerraformOutput is used to unmarshal the JSON output of the terraform command
type TerraformOutput struct {
	InstancePublicIP struct {
		Value string `json:"value"`
	} `json:"instance_public_ip"`
}

// PlistData holds the data to be filled in the plist template
type PlistData struct {
	Label         string
	IP            string
	Args          string
	Path          string
	Cwd           string
	LogPath       string
	StartInterval string
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
  <string>/usr/local/bin:{{.Path}}:/usr/bin:/bin:/usr/sbin:/sbin:</string>
</dict>

  <key>StartInterval</key>
  <integer>{{.StartInterval}}</integer>

  <key>StandardOutPath</key>
  <string>{{.LogPath}}</string>

  <key>StandardErrorPath</key>
  <string>{{.LogPath}}</string>

  <key>WorkingDirectory</key>
  <string>{{.Cwd}}</string>

  <key>RunAtLoad</key>
  <false/>
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

	// commands less than 10 chars are not worth saving
	MAX_LEN := 10
	// Write the unique lines to the summary file
	for _, line := range uniqueLines {

		if len(line) < MAX_LEN {
			continue
		}
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

// Config struct to hold the values
type Config struct {
	IP       string
	Label    string
	CWD      string
	ShowFull bool
	Install  bool
	Delay    time.Duration
}

func main() {
	config := Config{}
	flag.StringVar(&config.Label, "label", "com.tarsnap", "The label for the .plist file")
	flag.StringVar(&config.CWD, "cwd", ".", "Working directory for the launchd task")
	flag.BoolVar(&config.ShowFull, "show-full", false, "Show the unique list of lines to stdout")
	flag.BoolVar(&config.Install, "install", false, "Install launchd plist and exit")
	flag.DurationVar(&config.Delay, "delay", 10*time.Minute, "Delay between successive fetches")
	flag.Parse()

	if config.Install {
		err := setup(config)
		if err != nil {
			panic(err)
		}
		return
	}

	dowork()
}

func getip() (string, error) {
	log.Println("Running Terraform command to get output...")

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("Error getting current working directory:", err)
		return "", err
	}

	tfpath := filepath.Join(cwd, "./terraform")

	cmdName := "terraform"
	args := []string{fmt.Sprintf("-chdir=%s", tfpath), "output", "-json"}

	// Prepare the command
	cmd := exec.Command(cmdName, args...)

	// Print string representation of the command
	log.Printf("Executing command: %s %s", cmdName, strings.Join(args, " "))

	// Run the command
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	// Process output
	// log.Println(string(out))

	log.Println("Parsing JSON output...")

	var tfOutput TerraformOutput
	err = json.Unmarshal(out, &tfOutput)
	if err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if !isValidIPv4(tfOutput.InstancePublicIP.Value) {
		log.Fatalf("'%s' is not a valid ip", tfOutput.InstancePublicIP.Value)
	}

	return tfOutput.InstancePublicIP.Value, err
}

func setup(config Config) error {
	// func setup(config struct) (string, error) {
	log.SetFlags(log.LstdFlags | log.Llongfile)
	log.Println("This is a test message")

	var absCwd string

	// If --show-full flag is provided, only show the unique list of bash lines
	if config.ShowFull {
		logDir := "./data/bash_history"
		uniqueLines := getUniqueBashLines(logDir)
		for _, line := range uniqueLines {
			fmt.Println(line)
		}
		return nil
	}

	// Expand cwd into an absolute path
	absCwd, err := filepath.Abs(config.CWD)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	ip, err := getip()
	if err != nil {
		panic(err)
	}

	if ip == "" {
		log.Fatal("cound not get ip, quitting")
	}

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

	launctlTask := fmt.Sprintf("%s.%s", config.Label, ip)

	// get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("Error getting current directory:", err)
		return err
	}
	fmt.Println("CWD:", cwd)

	// Get the user's home directory
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return err
	}

	// Join the home directory with the target path
	LaunchAgentsDir := filepath.Join(home, "Library/LaunchAgents/")

	fmt.Println(LaunchAgentsDir)

	// concatenate cwd with the plist file name
	plist := fmt.Sprintf("%s/%s.plist", LaunchAgentsDir, launctlTask)

	// Get the base name
	baseName := filepath.Base(plist)

	// Remove the extension
	baseNameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	fmt.Println(baseNameWithoutExt)

	data := PlistData{
		Label:         baseNameWithoutExt,
		IP:            ip,
		StartInterval: strconv.Itoa(int(config.Delay.Seconds())),
		Args:          absExePath,
		Path:          exeDir,
		Cwd:           absCwd,
		LogPath:       fmt.Sprintf("/tmp/%s.log", "tarsnap"),
	}

	file, err := os.Create(plist)
	if err != nil {
		log.Fatalf("Failed to create .plist file: %v", err)
	}
	defer file.Close()

	err = tmpl.Execute(file, data)
	if err != nil {
		log.Fatalf("Failed to execute template: %v", err)
	}

	log.Println("Successfully created launchd .plist file.")

	// removeLaunchdTarsnap(launctlTask)
	loadLaunchdTarsnap(launctlTask, plist)
	searchLaunchdList(launctlTask)
	time.Sleep(500 * time.Millisecond)
	searchLaunchdList(launctlTask)

	return nil
}

func dowork() {
	// Set SSH user
	user := "root"

	ip, err := getip()
	if err != nil {
		panic(err)
	}

	// Create local directory if it does not exist
	localDir := "./data/bash_history"

	localDir, err = filepath.Abs(localDir)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println(localDir)

	// Append current timestamp to the filename
	localFile := fmt.Sprintf("%s/bash_history_%s.txt", localDir, time.Now().Format("20060102_150405"))
	absLocalFile, err := filepath.Abs(localFile)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	err = os.MkdirAll(localDir, 0o755)
	if err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}

	// Create the command with scp and arguments
	cmd := exec.Command("scp", "-o", "ConnectTimeout=10", fmt.Sprintf("%s@%s:~/.bash_history", user, ip), absLocalFile)

	log.Println("Copying remote bash history file to the local machine...")

	// Logging the command
	log.Printf("Executing command: scp -o ConnectTimeout=10 %s@%s:~/.bash_history %s\n", user, ip, absLocalFile)

	// Run the command and capture the combined output
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	// First, declare a bytes.Buffer
	var out bytes.Buffer

	// Then, write the output to the buffer
	_, err = out.Write(outBytes)
	if err != nil {
		log.Fatalf("Failed to write to buffer: %v", err)
	}

	// To print the content of the buffer, you can convert it to a string:
	log.Println(out.String())

	log.Println(out) // Print the output

	if err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	log.Println("Successfully copied remote bash history file to the local machine.")

	log.Println("Output from the scp command:")

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

func searchLaunchdList(launctlTask string) {
	cmd := exec.Command("launchctl", "list")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	lines := strings.Split(out.String(), "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, launctlTask) {
			fmt.Printf("%s\n", line)
			found = true
			break
		}
	}

	if found {
		fmt.Printf("%s found, load was successful\n", launctlTask)
	} else {
		fmt.Printf("%s not found, load failed\n", launctlTask)
	}
}

func loadLaunchdTarsnap(launctlTask, plist string) {
	fmt.Printf("running command launchctl load %s\n", plist)
	cmd := exec.Command("launchctl", "load", plist)
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func isValidIPv4(ip string) bool {
	parsedIP, err := netaddr.ParseIP(ip)
	if err != nil {
		return false
	}
	return parsedIP.Is4()
}
