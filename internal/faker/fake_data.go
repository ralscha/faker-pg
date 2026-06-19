package faker

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/brianvoe/gofakeit/v7"
)

type dataFaker struct {
	fullNameRules  map[string]fakeDataRule
	tableNameRules map[string]fakeDataRule
	columnRules    map[string]fakeDataRule
	regexRules     []fakeDataRule
}

type fakeDataRule struct {
	selector     string
	functionName string
	lookupName   string
	info         gofakeit.Info
	params       gofakeit.MapParams
	regex        *regexp.Regexp
}

func newDataFaker(configured map[string]string) (*dataFaker, error) {
	if len(configured) == 0 {
		return nil, nil
	}

	faker := &dataFaker{
		fullNameRules:  make(map[string]fakeDataRule),
		tableNameRules: make(map[string]fakeDataRule),
		columnRules:    make(map[string]fakeDataRule),
	}

	selectors := make([]string, 0, len(configured))
	for selector := range configured {
		selectors = append(selectors, selector)
	}
	slices.Sort(selectors)

	for _, selector := range selectors {
		rule, target, err := compileFakeDataRule(selector, configured[selector])
		if err != nil {
			return nil, err
		}
		switch target {
		case "full":
			faker.fullNameRules[normalizeFilterName(selector)] = rule
		case "table":
			faker.tableNameRules[normalizeFilterName(selector)] = rule
		case "column":
			faker.columnRules[normalizeFilterName(selector)] = rule
		default:
			faker.regexRules = append(faker.regexRules, rule)
		}
	}

	return faker, nil
}

func compileFakeDataRule(selector string, functionConfig string) (fakeDataRule, string, error) {
	selector = strings.TrimSpace(selector)
	functionConfig = strings.TrimSpace(functionConfig)
	if selector == "" {
		return fakeDataRule{}, "", fmt.Errorf("fake-data selector cannot be empty")
	}
	if functionConfig == "" {
		return fakeDataRule{}, "", fmt.Errorf("fake-data function for %q cannot be empty", selector)
	}

	functionName, rawParams := parseFakeFunctionConfig(functionConfig)
	lookupName, info := resolveFakeFunction(functionName)
	if info == nil {
		return fakeDataRule{}, "", fmt.Errorf("fake-data %q uses unknown gofakeit function %q", selector, functionName)
	}
	params, err := buildFakeFunctionParams(selector, functionName, info, rawParams)
	if err != nil {
		return fakeDataRule{}, "", err
	}
	sample, err := info.Generate(gofakeit.New(1), cloneFakeParams(params), info)
	if err != nil {
		return fakeDataRule{}, "", fmt.Errorf("fake-data %q could not initialize gofakeit function %q: %w", selector, functionName, err)
	}
	if !supportedFakeValue(sample) {
		return fakeDataRule{}, "", fmt.Errorf("fake-data %q uses unsupported gofakeit function %q: output type %T is not supported", selector, functionName, sample)
	}

	rule := fakeDataRule{
		selector:     selector,
		functionName: functionName,
		lookupName:   lookupName,
		info:         *info,
		params:       params,
	}

	if target, normalized, ok := exactFakeDataTarget(selector); ok {
		rule.selector = normalized
		return rule, target, nil
	}

	re, err := regexp.Compile("(?i)^" + normalizeRegexSelector(selector) + "$")
	if err != nil {
		return fakeDataRule{}, "", fmt.Errorf("fake-data %q has invalid regex: %w", selector, err)
	}
	rule.regex = re
	return rule, "regex", nil
}

func exactFakeDataTarget(selector string) (string, string, bool) {
	normalized := normalizeFilterName(selector)
	if normalized == "" {
		return "", "", false
	}
	parts := strings.Split(normalized, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return "", "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", "", false
		}
		for _, r := range part {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return "", "", false
			}
		}
	}
	if len(parts) == 3 {
		return "full", normalized, true
	}
	if len(parts) == 2 {
		return "table", normalized, true
	}
	return "column", normalized, true
}

func normalizeRegexSelector(selector string) string {
	return normalizeFilterName(selector)
}

func parseFakeFunctionConfig(value string) (string, []string) {
	parts := strings.Split(value, ";")
	functionName := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return functionName, nil
	}
	params := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		params = append(params, strings.TrimSpace(part))
	}
	return functionName, params
}

func buildFakeFunctionParams(selector string, functionName string, info *gofakeit.Info, values []string) (gofakeit.MapParams, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(info.Params) == 0 {
		return nil, fmt.Errorf("fake-data %q passes parameters to gofakeit function %q, but it does not accept any", selector, functionName)
	}

	params := gofakeit.MapParams{}
	valueIndex := 0
	for paramIndex, param := range info.Params {
		if valueIndex >= len(values) {
			break
		}

		remainingParams := len(info.Params) - paramIndex
		remainingValues := len(values) - valueIndex
		assignCount := 1
		if strings.HasPrefix(param.Type, "[]") {
			assignCount = max(remainingValues-(remainingParams-1), 1)
		}

		for i := 0; i < assignCount && valueIndex < len(values); i++ {
			params.Add(param.Field, values[valueIndex])
			valueIndex++
		}
	}

	if valueIndex != len(values) {
		return nil, fmt.Errorf("fake-data %q passes %d parameters to gofakeit function %q, but it only accepts %d", selector, len(values), functionName, len(info.Params))
	}

	return params, nil
}

func cloneFakeParams(params gofakeit.MapParams) *gofakeit.MapParams {
	if len(params) == 0 {
		return nil
	}
	cloned := gofakeit.MapParams{}
	for key, values := range params {
		clonedValues := append([]string(nil), values...)
		cloned[key] = clonedValues
	}
	return &cloned
}

func resolveFakeFunction(name string) (string, *gofakeit.Info) {
	normalizedName := normalizeFakeFunctionName(name)
	if normalizedName != "" {
		if info := gofakeit.GetFuncLookup(normalizedName); info != nil {
			return normalizedName, info
		}
	}

	if categoryName, functionName, ok := strings.Cut(strings.TrimSpace(name), "."); ok {
		normalizedCategory := normalizeFakeFunctionName(categoryName)
		normalizedFunction := normalizeFakeFunctionName(functionName)
		if normalizedCategory != "" && normalizedFunction != "" {
			if info := gofakeit.GetFuncLookup(normalizedFunction); info != nil {
				if slices.Contains(fakeFunctionCategoryCandidates(normalizedCategory), normalizeFakeFunctionName(info.Category)) {
					return normalizedFunction, info
				}
			}
		}
	}
	return "", nil
}

func fakeFunctionCategoryCandidates(category string) []string {
	aliases := map[string][]string{
		"words":    {"text", "word", "words"},
		"foods":    {"food"},
		"colors":   {"color"},
		"images":   {"image"},
		"language": {"language", "languages"},
		"datetime": {"datetime"},
	}
	if candidates, ok := aliases[category]; ok {
		return candidates
	}
	return []string{category}
}

func supportedFakeValue(value any) bool {
	if value == nil {
		return true
	}
	switch value.(type) {
	case string, []byte, bool, time.Time,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

func supportedFakeOutput(output string) bool {
	switch output {
	case "string", "[]string", "[]byte", "byte", "net.IP", "bool", "time", "time.Time",
		dtInt, dtInt8, "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float", "float32", "float64",
		"[]int", "[]uint":
		return true
	default:
		return false
	}
}

func (f *dataFaker) fakeValue(faker *gofakeit.Faker, table tableMeta, col columnMeta) (any, bool, error) {
	if f == nil {
		return nil, false, nil
	}
	if faker == nil {
		faker = gofakeit.GlobalFaker
	}
	rule, ok := f.matchRule(table, col)
	if !ok {
		return nil, false, nil
	}
	value, err := rule.info.Generate(faker, cloneFakeParams(rule.params), &rule.info)
	return value, true, err
}

func (f *dataFaker) matchRule(table tableMeta, col columnMeta) (fakeDataRule, bool) {
	if f == nil {
		return fakeDataRule{}, false
	}

	fullName := normalizeFilterName(table.Schema + "." + table.Name + "." + col.Name)
	tableName := normalizeFilterName(table.Name + "." + col.Name)
	columnName := normalizeFilterName(col.Name)

	if rule, ok := f.fullNameRules[fullName]; ok {
		return rule, true
	}
	if rule, ok := f.tableNameRules[tableName]; ok {
		return rule, true
	}
	if rule, ok := f.columnRules[columnName]; ok {
		return rule, true
	}
	for _, rule := range f.regexRules {
		if rule.regex != nil && rule.regex.MatchString(fullName) {
			return rule, true
		}
	}
	return fakeDataRule{}, false
}

func matchingOutputTypes(dataType string) []string {
	dt := strings.ToLower(dataType)

	if strings.Contains(dt, "char") || strings.Contains(dt, "text") ||
		dt == dtName || dt == dtUUID {
		return []string{"string", "[]string", "[]byte", "byte", "net.IP"}
	}

	if dt == "integer" || dt == dtInt || dt == "bigint" || dt == "smallint" ||
		dt == "int2" || dt == "int4" || dt == dtInt8 || strings.Contains(dt, "serial") {
		return []string{dtInt, dtInt8, "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"[]int", "[]uint"}
	}

	if strings.Contains(dt, "float") || strings.Contains(dt, "double") ||
		strings.Contains(dt, "numeric") || strings.Contains(dt, "decimal") ||
		strings.Contains(dt, "real") || strings.Contains(dt, "money") {
		return []string{"float", "float32", "float64",
			dtInt, dtInt8, "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64"}
	}

	if strings.Contains(dt, "bool") {
		return []string{"bool"}
	}

	if strings.Contains(dt, "date") || strings.Contains(dt, "timestamp") ||
		strings.HasPrefix(dt, "time ") {
		return []string{"time", "time.Time"}
	}

	return nil
}

func availableFakeFunctionOptions() []fakeFunctionOption {
	probe := gofakeit.New(1)
	var options []fakeFunctionOption
	for name, info := range gofakeit.FuncLookups {
		sample, err := info.Generate(probe, nil, &info)
		if err != nil && (len(info.Params) == 0 || !supportedFakeOutput(info.Output)) {
			continue
		}
		if err == nil && !supportedFakeValue(sample) {
			continue
		}
		opt := fakeFunctionOption{
			LookupName:  name,
			Display:     info.Display,
			Category:    info.Category,
			Description: info.Description,
			Example:     fmt.Sprintf("%v", info.Example),
			SearchText:  strings.ToLower(info.Category + " " + info.Display + " " + info.Description + " " + name),
			Output:      info.Output,
		}
		for _, p := range info.Params {
			opt.Params = append(opt.Params, fakeParam{
				Field:       p.Field,
				Description: p.Description,
				Type:        p.Type,
				Optional:    p.Optional,
			})
		}
		options = append(options, opt)
	}
	slices.SortFunc(options, func(a, b fakeFunctionOption) int {
		return strings.Compare(a.LookupName, b.LookupName)
	})
	return options
}

func buildFakeFunctionConfigFromEntry(entry tuiFakeDataEntry) string {
	return buildFakeFunctionConfig(entry.FunctionName, entry.FunctionParams)
}

func countExactFakeDataRules(mappings map[string]string) int {
	count := 0
	for selector := range mappings {
		if _, _, ok := exactFakeDataTarget(selector); ok {
			count++
		}
	}
	return count
}

func replaceValue(col columnMeta, fakeValue any) any {
	if fakeValue == nil {
		return nil
	}

	switch col.UDTName {
	case "varchar", "bpchar", "text", dtName:
		s, ok := fakeValue.(string)
		if !ok {
			s = fmt.Sprintf("%v", fakeValue)
		}
		if col.MaxLength > 0 {
			s = truncateString(s, col.MaxLength)
		}
		return s
	case "int2", "int4", dtInt8:
		return fakeValue
	case "float4", "float8", "numeric":
		return fakeValue
	case "bool":
		return fakeValue
	case "date", "timestamp", "timestamptz", "time", "timetz":
		return fakeValue
	case dtUUID:
		s, ok := fakeValue.(string)
		if !ok {
			s = fmt.Sprintf("%v", fakeValue)
		}
		return s
	default:
		if s, ok := fakeValue.(string); ok {
			if col.MaxLength > 0 {
				s = truncateString(s, col.MaxLength)
			}
			return s
		}
		return fmt.Sprintf("%v", fakeValue)
	}
}
