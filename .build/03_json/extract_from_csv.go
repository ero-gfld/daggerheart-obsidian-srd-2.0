package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type regularField struct {
	orig string
	norm string
}

type groupedField struct {
	index int
	sub   string
	subIndex int
	orig  string
}

func normalizeHeader(header string) string {
	header = strings.TrimSpace(strings.ToLower(header))
	var b strings.Builder
	underscore := false
	for _, r := range header {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			underscore = false
			continue
		}
		if !underscore {
			b.WriteRune('_')
			underscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	out = regexp.MustCompile(`_+`).ReplaceAllString(out, "_")
	return out
}

func groupDigitFields(fieldnames []string) ([]regularField, map[string][]groupedField) {
	regular := []regularField{}
	grouped := map[string][]groupedField{}
	patternDot := regexp.MustCompile(`^(.*?)[ _-](\d+)\.(\d+)(?:[ _-](.*))?$`)
	patternTwo := regexp.MustCompile(`^(.*?)[ _-](\d+)[ _-](.*?)[ _-](\d+)(?:[ _-](.*))?$`)
	patternOne := regexp.MustCompile(`^(.*?)[ _-](\d+)[ _-](.*)$`)
	for _, orig := range fieldnames {
		if m := patternDot.FindStringSubmatch(orig); m != nil {
			prefix, indexStr, subIndexStr, subfield := m[1], m[2], m[3], m[4]
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				regular = append(regular, regularField{orig: orig, norm: normalizeHeader(orig)})
				continue
			}
			subIndex, err := strconv.Atoi(subIndexStr)
			if err != nil {
				regular = append(regular, regularField{orig: orig, norm: normalizeHeader(orig)})
				continue
			}
			groupKey := normalizeHeader(prefix)
			subKey := normalizeHeader(subfield)
			if subKey == "" {
				subKey = "option"
			}
			grouped[groupKey] = append(grouped[groupKey], groupedField{
				index:    index,
				sub:      subKey,
				subIndex: subIndex,
				orig:     orig,
			})
			continue
		}
		if m := patternTwo.FindStringSubmatch(orig); m != nil {
			prefix, indexStr, mid, subIndexStr, tail := m[1], m[2], m[3], m[4], m[5]
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				regular = append(regular, regularField{orig: orig, norm: normalizeHeader(orig)})
				continue
			}
			subIndex, err := strconv.Atoi(subIndexStr)
			if err != nil {
				regular = append(regular, regularField{orig: orig, norm: normalizeHeader(orig)})
				continue
			}
			subfield := strings.TrimSpace(mid)
			if strings.TrimSpace(tail) != "" {
				subfield = strings.TrimSpace(subfield + " " + tail)
			}
			groupKey := normalizeHeader(prefix)
			subKey := normalizeHeader(subfield)
			if subKey == "" {
				subKey = "value"
			}
			grouped[groupKey] = append(grouped[groupKey], groupedField{
				index: index,
				sub:   subKey,
				subIndex: subIndex,
				orig:  orig,
			})
			continue
		}
		m := patternOne.FindStringSubmatch(orig)
		if m == nil {
			regular = append(regular, regularField{orig: orig, norm: normalizeHeader(orig)})
			continue
		}
		prefix, indexStr, subfield := m[1], m[2], m[3]
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			regular = append(regular, regularField{orig: orig, norm: normalizeHeader(orig)})
			continue
		}
		groupKey := normalizeHeader(prefix)
		subKey := normalizeHeader(subfield)
		if subKey == "" {
			subKey = "value"
		}
		grouped[groupKey] = append(grouped[groupKey], groupedField{
			index: index,
			sub:   subKey,
			orig:  orig,
		})
	}
	for key := range grouped {
		sort.Slice(grouped[key], func(i, j int) bool {
			return grouped[key][i].index < grouped[key][j].index
		})
	}
	return regular, grouped
}

func readCSV(path string) ([]string, [][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, nil, err
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}

	rows := [][]string{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
	return header, rows, nil
}

func csvToJSON(csvPath, jsonPath string) error {
	header, rows, err := readCSV(csvPath)
	if err != nil {
		return err
	}
	if len(header) == 0 {
		return nil
	}
	regular, grouped := groupDigitFields(header)

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		record := map[string]any{}
		// Regular fields
		for _, f := range regular {
			idx := indexOf(header, f.orig)
			if idx < 0 || idx >= len(row) {
				continue
			}
			val := strings.Trim(row[idx], " \t\r\n")
			if val == "" {
				continue
			}
			record[f.norm] = val
		}

		// Grouped fields
		for groupKey, fields := range grouped {
			groupRows := map[int]map[string]any{}
			for _, gf := range fields {
				idx := indexOf(header, gf.orig)
				if idx < 0 || idx >= len(row) {
					continue
				}
				val := strings.Trim(row[idx], " \t\r\n")
				if val == "" {
					continue
				}
				if _, ok := groupRows[gf.index]; !ok {
					groupRows[gf.index] = map[string]any{}
				}
				if gf.subIndex > 0 {
					subMap, ok := groupRows[gf.index][gf.sub].(map[int]string)
					if !ok {
						subMap = map[int]string{}
					}
					subMap[gf.subIndex] = val
					groupRows[gf.index][gf.sub] = subMap
				} else {
					groupRows[gf.index][gf.sub] = val
				}
			}
			if len(groupRows) == 0 {
				continue
			}
			indexes := make([]int, 0, len(groupRows))
			for idx := range groupRows {
				indexes = append(indexes, idx)
			}
			sort.Ints(indexes)
			groupList := make([]any, 0, len(indexes))
			for _, idx := range indexes {
				if len(groupRows[idx]) == 0 {
					continue
				}
				entry := map[string]any{}
				for key, val := range groupRows[idx] {
					switch typed := val.(type) {
					case string:
						entry[key] = typed
					case map[int]string:
						subIndexes := make([]int, 0, len(typed))
						for subIdx := range typed {
							subIndexes = append(subIndexes, subIdx)
						}
						sort.Ints(subIndexes)
						list := make([]string, 0, len(subIndexes))
						for _, subIdx := range subIndexes {
							list = append(list, typed[subIdx])
						}
						if len(list) > 0 {
							entry[key] = list
						}
					}
				}
				if len(entry) > 0 {
					if len(entry) == 1 {
						if list, ok := entry["option"].([]string); ok {
							groupList = append(groupList, list)
							continue
						}
					}
					groupList = append(groupList, entry)
				}
			}
			if len(groupList) > 0 {
				record[groupKey] = groupList
			}
		}

		if len(record) > 0 {
			out = append(out, record)
		}
	}

	if err := os.MkdirAll(filepath.Dir(jsonPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte{0xEF, 0xBB, 0xBF}, data...)
	return os.WriteFile(jsonPath, data, 0644)
}

func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}

func main() {
	csvDir := ".build/02_csv"
	jsonDir := ".build/03_json"
	entries, err := os.ReadDir(csvDir)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", csvDir, err)
		return
	}
	if err := os.MkdirAll(jsonDir, 0755); err != nil {
		fmt.Printf("Error creating %s: %v\n", jsonDir, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		csvPath := filepath.Join(csvDir, entry.Name())
		jsonPath := filepath.Join(jsonDir, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))+".json")
		fmt.Printf("Converting %s -> %s\n", csvPath, jsonPath)
		if err := csvToJSON(csvPath, jsonPath); err != nil {
			fmt.Printf("Error converting %s: %v\n", csvPath, err)
		}
	}
}
