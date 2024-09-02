package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	rootDir := flag.String("rootDir", "./", "The root directory to search for .tfvars and .tf files")
	flag.Parse()

	tfvarsPaths, err := findTFVarsFiles(*rootDir, ".tfvars")
	if err != nil {
		fmt.Printf("Error finding .tfvars files: %v\n", err)
		os.Exit(1)
	}

	tfPaths, err := findTFVarsFiles(*rootDir, ".tf")
	if err != nil {
		fmt.Printf("Error finding .tf files: %v\n", err)
		os.Exit(1)
	}

	mergedMap := make(map[string]any)
	for _, tfvarsPath := range tfvarsPaths {
		tfvarsMap, err := parseTFVarsFile(tfvarsPath)
		if err != nil {
			fmt.Printf("Error parsing .tfvars file (%s): %v\n", tfvarsPath, err)
			os.Exit(1)
		}

		for key, value := range tfvarsMap {
			mergedMap[key] = value
		}
	}

	if err := yaml.NewEncoder(os.Stdout).Encode(mergedMap); err != nil {
		fmt.Printf("Error encoding YAML: %v\n", err)
		os.Exit(1)
	}

	for _, tfPath := range tfPaths {
		tfContent, err := os.ReadFile(tfPath)
		if err != nil {
			fmt.Printf("Error reading .tf file (%s): %v\n", tfPath, err)
			os.Exit(1)
		}

		replacedContent, err := replaceVars(string(tfContent), mergedMap)
		if err != nil {
			fmt.Printf("Error replacing vars in .tf file (%s): %v\n", tfPath, err)
			os.Exit(1)
		}

		if err := os.WriteFile(tfPath, []byte(replacedContent), 0644); err != nil {
			fmt.Printf("Error writing to .tf file (%s): %v\n", tfPath, err)
			os.Exit(1)
		}
	}
}

// findTFVarsFiles searches for all .tfvars files within the given directory and its subdirectories.
func findTFVarsFiles(rootDir string, ext string) ([]string, error) {
	var tfvarsFiles []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the file has a .tfvars extension
		if filepath.Ext(info.Name()) == ext {
			tfvarsFiles = append(tfvarsFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return tfvarsFiles, nil
}

// parseTFVarsFile parses a .tfvars file and returns a map of the key-value pairs.
func parseTFVarsFile(filePath string) (map[string]any, error) {
	tfvarsMap := make(map[string]any)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines or comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		// Split the line into key and value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line format: %s", line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Handle objects (map values)
		if strings.HasPrefix(value, "{") {
			nestedMap, remainingLines, err := parseObject(scanner, value)
			if err != nil {
				return nil, err
			}
			tfvarsMap[key] = nestedMap
			scanner = remainingLines
		} else if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			// Handle string values
			value = strings.Trim(value, "\"")
			tfvarsMap[key] = value
		} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			// Handle list values (e.g., value = ["item1", "item2"])
			value = strings.Trim(value, "[]")
			items := strings.Split(value, ",")
			for i := range items {
				items[i] = strings.TrimSpace(strings.Trim(items[i], "\""))
			}
			tfvarsMap[key] = items
		} else if num, err := strconv.ParseFloat(value, 64); err == nil {
			tfvarsMap[key] = num
		} else if value == "true" || value == "false" {
			tfvarsMap[key], _ = strconv.ParseBool(value)
		} else {
			tfvarsMap[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return tfvarsMap, nil
}

func parseObject(scanner *bufio.Scanner, firstLine string) (map[string]any, *bufio.Scanner, error) {
	objMap := make(map[string]any)
	firstLine = strings.TrimSpace(firstLine[1:]) // Remove opening brace

	if firstLine != "" {
		if strings.HasSuffix(firstLine, "}") {
			// The object is a single line like {key="value"}
			firstLine = strings.TrimSuffix(firstLine, "}")
			subparts := strings.SplitN(firstLine, "=", 2)
			if len(subparts) == 2 {
				key := strings.TrimSpace(subparts[0])
				value := strings.TrimSpace(subparts[1])
				if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
					objMap[key] = strings.Trim(value, "\"")
				} else {
					return nil, nil, fmt.Errorf("invalid object value format: %s", firstLine)
				}
			}
			return objMap, scanner, nil
		}

		// The object starts on the same line
		subparts := strings.SplitN(firstLine, "=", 2)
		if len(subparts) == 2 {
			key := strings.TrimSpace(subparts[0])
			value := strings.TrimSpace(subparts[1])
			if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
				objMap[key] = strings.Trim(value, "\"")
			}
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "}") {
			return objMap, scanner, nil
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("invalid object line format: %s", line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if strings.HasPrefix(value, "{") {
			// Handle nested objects
			nestedMap, remainingLines, err := parseObject(scanner, value)
			if err != nil {
				return nil, nil, err
			}
			objMap[key] = nestedMap
			scanner = remainingLines
		} else if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			objMap[key] = strings.Trim(value, "\"")
		} else {
			objMap[key] = value
		}
	}

	return nil, nil, fmt.Errorf("object not properly closed")
}

// replaceVars replaces occurrences of var.key in the input string with the corresponding values from the varsMap.
// If the value is not a boolean, number, or explicitly quoted string, it adds double quotes around the value.
func replaceVars(input string, varsMap map[string]any) (string, error) {
	// Regular expression to find occurrences of var.key
	re := regexp.MustCompile(`var\.(\w+)`)

	// Replace each found var.key with the corresponding value from the map
	result := re.ReplaceAllStringFunc(input, func(match string) string {
		// Extract the key name from var.key
		key := re.FindStringSubmatch(match)[1]

		// Get the corresponding value from the map
		if value, exists := varsMap[key]; exists {
			switch v := value.(type) {
			case bool:
				// If the value is a boolean, return "true" or "false"
				return strconv.FormatBool(v)
			case int:
				// If the value is an int, return the formatted integer
				return strconv.Itoa(v)
			case int64:
				// If the value is an int64, return the formatted integer
				return strconv.FormatInt(v, 10)
			case float64:
				// If the value is a float64, return the formatted float with default precision
				return strconv.FormatFloat(v, 'f', -1, 64)
			case float32:
				// If the value is a float32, return the formatted float with default precision
				return strconv.FormatFloat(float64(v), 'f', -1, 32)
			case string:
				// If the value is a string, decide whether to quote it or not
				if _, err := strconv.ParseBool(v); err == nil {
					return v
				}
				if _, err := strconv.ParseFloat(v, 64); err == nil {
					return v
				}
				// Otherwise, wrap the string in double quotes
				return fmt.Sprintf("\"%s\"", v)
			default:
				// If the value is any other type, convert to string and wrap in quotes
				return fmt.Sprintf("\"%v\"", v)
			}
		}

		// If the key is not found in the map, leave the match as is
		return match
	})

	return result, nil
}
