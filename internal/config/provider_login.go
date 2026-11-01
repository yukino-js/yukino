package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/google/uuid"

	"github.com/hangtiancheng/yukino.go/apps/yukino/internal/storage"
)

var digitsOnly = regexp.MustCompile(`^\d+$`)

// tokenLimit mirrors the login schema's numeric preprocessing: unset or blank
// strings fall back to the default; whole-number strings are accepted; the
// result must be an integer within [min, max].
func tokenLimit(value any, fallback, min, max int) (int, error) {
	var number float64
	switch v := value.(type) {
	case nil:
		number = float64(fallback)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			number = float64(fallback)
			break
		}
		if !digitsOnly.MatchString(trimmed) {
			return 0, fmt.Errorf("Enter a whole number")
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, fmt.Errorf("Enter a whole number")
		}
		number = parsed
	default:
		n, err := asNumber(v)
		if err != nil {
			return 0, fmt.Errorf("Enter a whole number")
		}
		number = n
	}
	if number != float64(int64(number)) || number < float64(min) || number > float64(max) {
		return 0, fmt.Errorf("expected a whole number between %d and %d", min, max)
	}
	return int(number), nil
}

// ParseProviderLogin validates login-form input, fills optional defaults, and
// persists an explicit effective thinking level while retaining all capability
// metadata.
func ParseProviderLogin(input map[string]any) (ProviderConfig, error) {
	p, err := parseProviderConfig(input)
	if err != nil {
		return p, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return p, fmt.Errorf("name: Name is required")
	}
	parsedURL, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return p, fmt.Errorf("base_url: Enter a valid HTTP(S) URL")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return p, fmt.Errorf("api_key: API key is required")
	}
	if strings.TrimSpace(p.Model) == "" {
		return p, fmt.Errorf("model: Model is required")
	}
	contextWindow, err := tokenLimit(input["context_window"], DefaultContextWindow, 1_000, 10_000_000)
	if err != nil {
		return p, fmt.Errorf("context_window: %w", err)
	}
	maxOutputTokens, err := tokenLimit(input["max_output_tokens"], DefaultMaxOutputTokens, 1, 1_000_000)
	if err != nil {
		return p, fmt.Errorf("max_output_tokens: %w", err)
	}
	if maxOutputTokens > contextWindow {
		return p, fmt.Errorf("max_output_tokens: Max output tokens must not exceed the context window")
	}
	p.ContextWindow = float64(contextWindow)
	p.MaxOutputTokens = float64(maxOutputTokens)
	p.Thinking = GetThinkingLevel(&p)
	return p, nil
}

// readConfigRaw reads the raw global config as an ordered record; an absent
// file yields an empty one.
func readConfigRaw(path string) (yaml.MapSlice, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return yaml.MapSlice{}, nil
		}
		return nil, err
	}
	data, err := storage.ReadPrivateFileSync(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.UnmarshalWithOptions(data, &raw, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("Unable to read existing %s; it has not been changed.", path)
	}
	if raw == nil {
		return yaml.MapSlice{}, nil
	}
	slice, ok := raw.(yaml.MapSlice)
	if !ok {
		return nil, fmt.Errorf("Unable to read existing %s; it has not been changed.", path)
	}
	return slice, nil
}

// writeConfigAtomic atomically writes the global config with 0600 permissions.
func writeConfigAtomic(path string, raw yaml.MapSlice) error {
	directory := filepath.Dir(path)
	if err := storage.MkdirPrivateSync(directory); err != nil {
		return err
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	temporary := filepath.Join(directory, ".config-"+uuid.NewString()+".tmp")
	if err := storage.WritePrivateFileSync(temporary, data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

func rawGet(raw yaml.MapSlice, key string) (any, bool) {
	for _, item := range raw {
		if k, ok := item.Key.(string); ok && k == key {
			return item.Value, true
		}
	}
	return nil, false
}

func rawSet(raw yaml.MapSlice, key string, value any) yaml.MapSlice {
	for i, item := range raw {
		if k, ok := item.Key.(string); ok && k == key {
			raw[i].Value = value
			return raw
		}
	}
	return append(raw, yaml.MapItem{Key: key, Value: value})
}

func rawProviders(raw yaml.MapSlice) ([]yaml.MapSlice, error) {
	value, ok := rawGet(raw, "providers")
	if !ok || value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("providers: expected a list")
	}
	entries := make([]yaml.MapSlice, 0, len(list))
	for _, item := range list {
		rec, ok := item.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("providers: expected mapping entries")
		}
		entries = append(entries, rec)
	}
	return entries, nil
}

// recordToMap deep-converts an ordered YAML record into plain maps so the
// schema parsers can validate it.
func recordToMap(value any) any {
	switch v := value.(type) {
	case yaml.MapSlice:
		m := make(map[string]any, len(v))
		for _, item := range v {
			if k, ok := item.Key.(string); ok {
				m[k] = recordToMap(item.Value)
			}
		}
		return m
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = recordToMap(item)
		}
		return out
	default:
		return value
	}
}

func providerToRecord(p ProviderConfig) yaml.MapSlice {
	rec := yaml.MapSlice{}
	add := func(key string, value any) {
		rec = append(rec, yaml.MapItem{Key: key, Value: value})
	}
	add("name", p.Name)
	add("protocol", p.Protocol)
	add("base_url", p.BaseURL)
	add("model", p.Model)
	if p.APIKey != "" {
		add("api_key", p.APIKey)
	}
	if p.Thinking != "" {
		add("thinking", p.Thinking)
	}
	if p.Reasoning != nil {
		add("reasoning", *p.Reasoning)
	}
	if p.ThinkingLevelMap != nil {
		levelMap := yaml.MapSlice{}
		for _, level := range ThinkingLevels {
			override, ok := p.ThinkingLevelMap[level]
			if !ok {
				continue
			}
			if override.Null {
				levelMap = append(levelMap, yaml.MapItem{Key: level, Value: nil})
				continue
			}
			levelMap = append(levelMap, yaml.MapItem{Key: level, Value: override.Effort})
		}
		add("thinking_level_map", levelMap)
	}
	if p.ThinkingMode != "" {
		add("thinking_mode", p.ThinkingMode)
	}
	if p.ContextWindow != 0 {
		add("context_window", int(p.ContextWindow))
	}
	if p.MaxOutputTokens != 0 {
		add("max_output_tokens", int(p.MaxOutputTokens))
	}
	return append(rec, p.Extra...)
}

type SaveProviderResult struct {
	Provider  ProviderConfig
	Providers []ProviderConfig
	// Whether an existing entry with the same base_url was replaced.
	Replaced bool
	Path     string
}

// SaveProvider saves a provider to the single global config
// ($HOME/.yukino/config.yaml), retaining every currently available provider.
// base_url is the provider identity: an entry with the same base_url is
// replaced in place instead of appended, and names may repeat freely.
func SaveProvider(input map[string]any, available []ProviderConfig) (SaveProviderResult, error) {
	provider, err := ParseProviderLogin(input)
	if err != nil {
		return SaveProviderResult{}, err
	}
	path := GlobalConfigPath()
	raw, err := readConfigRaw(path)
	if err != nil {
		return SaveProviderResult{}, err
	}
	existing, err := rawProviders(raw)
	if err != nil {
		return SaveProviderResult{}, err
	}
	// Retain every currently available provider so an in-memory list never
	// loses entries that are not yet written to the file.
	current := append([]yaml.MapSlice{}, existing...)
	for _, entry := range available {
		found := false
		for _, stored := range current {
			if v, _ := rawGet(stored, "base_url"); v == entry.BaseURL {
				found = true
				break
			}
		}
		if !found {
			current = append(current, providerToRecord(entry))
		}
	}
	var storedEntries []yaml.MapSlice
	replaced := false
	for _, entry := range current {
		baseURL, _ := rawGet(entry, "base_url")
		if baseURL != provider.BaseURL {
			storedEntries = append(storedEntries, entry)
			continue
		}
		// Replace the first match in place and drop any further duplicates, so
		// one endpoint keeps exactly one entry.
		if replaced {
			continue
		}
		replaced = true
		storedEntries = append(storedEntries, providerToRecord(provider))
	}
	if !replaced {
		storedEntries = append(storedEntries, providerToRecord(provider))
	}
	providers := make([]ProviderConfig, 0, len(storedEntries))
	for _, entry := range storedEntries {
		rec, _ := recordToMap(entry).(map[string]any)
		p, err := parseProviderConfig(rec)
		if err != nil {
			return SaveProviderResult{}, err
		}
		providers = append(providers, p)
	}
	list := make([]any, len(storedEntries))
	for i, entry := range storedEntries {
		list[i] = entry
	}
	raw = rawSet(raw, "providers", list)
	if err := writeConfigAtomic(path, raw); err != nil {
		return SaveProviderResult{}, err
	}
	return SaveProviderResult{Provider: provider, Providers: providers, Replaced: replaced, Path: path}, nil
}

// persistProviderField persists a single field of every provider entry
// matching base_url. The endpoint is the provider identity, so all entries for
// it change together. It errors when the endpoint is absent so callers can
// surface a clear message.
func persistProviderField(baseURL, field, value string) error {
	path := GlobalConfigPath()
	raw, err := readConfigRaw(path)
	if err != nil {
		return err
	}
	providers, err := rawProviders(raw)
	if err != nil {
		return err
	}
	targets := 0
	changed := false
	for i, entry := range providers {
		if v, _ := rawGet(entry, "base_url"); v != baseURL {
			continue
		}
		targets++
		if current, _ := rawGet(entry, field); current != value {
			providers[i] = rawSet(entry, field, value)
			changed = true
		}
	}
	if targets == 0 {
		return fmt.Errorf("Provider with base URL %q not found in %s.", baseURL, path)
	}
	// Avoid rewriting (and reformatting) the file when nothing changes.
	if !changed {
		return nil
	}
	list := make([]any, len(providers))
	for i, entry := range providers {
		list[i] = entry
	}
	raw = rawSet(raw, "providers", list)
	return writeConfigAtomic(path, raw)
}

func PersistThinkingLevel(baseURL, level string) error {
	return persistProviderField(baseURL, "thinking", level)
}

// PersistModel persists the active model of a provider without touching any
// other field.
func PersistModel(baseURL, model string) error {
	return persistProviderField(baseURL, "model", model)
}

// PersistDefaultProvider persists the index of the provider the user selected
// last, so the next start selects it directly instead of showing the provider
// picker. No-op when the stored index already matches, so the file is not
// rewritten (and reformatted) on every selection.
func PersistDefaultProvider(index int) error {
	path := GlobalConfigPath()
	raw, err := readConfigRaw(path)
	if err != nil {
		return err
	}
	if current, ok := rawGet(raw, "default_provider"); ok {
		if n, err := asNumber(current); err == nil && int(n) == index {
			return nil
		}
	}
	raw = rawSet(raw, "default_provider", index)
	return writeConfigAtomic(path, raw)
}
