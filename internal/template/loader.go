// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/config"
)

const maxTemplateFileSize = 1 << 20 // 1MB

var templateNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// loadResult holds the outcome of a loadAll operation.
type loadResult struct {
	Registered map[string]*RegisteredTemplate
	Hashes     map[string]string
	Failures   []TemplateLoadFailure
}

// loadAll loads all template files and shared fragments from disk.
//
// Flow:
//  1. Load shared fragments from shared_dir (default: base_dir/_shared)
//  2. For each template in cfg.Templates: validate name, read file, parse, compile schemas
//  3. Any single template failure is recorded but does not block others
func loadAll(cfg config.SQLTemplatesConfig, funcMap template.FuncMap, logger *zap.Logger) (*loadResult, error) {
	result := &loadResult{
		Registered: make(map[string]*RegisteredTemplate),
		Hashes:     make(map[string]string),
	}

	// --- Step 1: Load shared fragments ---
	sharedDir := cfg.SharedDir
	if sharedDir == "" {
		sharedDir = filepath.Join(cfg.BaseDir, "_shared")
	}

	sharedTmpl, err := loadSharedFragments(sharedDir, funcMap, logger)
	if err != nil {
		// Non-fatal: log and continue with empty shared template
		logger.Warn("failed to load shared fragments, continuing without them",
			zap.String("shared_dir", sharedDir),
			zap.Error(err),
		)
		sharedTmpl = template.New("__shared__").Funcs(funcMap).Option("missingkey=error")
	}

	// --- Step 2: Load each configured template ---
	seen := make(map[string]bool, len(cfg.Templates))

	for _, tc := range cfg.Templates {
		name := tc.Name

		// 2a. Validate name format
		if !templateNameRe.MatchString(name) {
			logger.Error("invalid template name, skipping",
				zap.String("name", name),
				zap.String("pattern", templateNameRe.String()),
			)
			result.Failures = append(result.Failures, TemplateLoadFailure{
				Name:  name,
				Error: fmt.Sprintf("name %q does not match pattern %s", name, templateNameRe.String()),
			})
			continue
		}

		// 2b. Check name uniqueness
		if seen[name] {
			logger.Error("duplicate template name, skipping",
				zap.String("name", name),
			)
			result.Failures = append(result.Failures, TemplateLoadFailure{
				Name:  name,
				Error: fmt.Sprintf("duplicate template name %q", name),
			})
			continue
		}
		seen[name] = true

		// 2c. Construct full file path
		filePath := filepath.Join(cfg.BaseDir, tc.File)

		// 2d. Read file and check size
		content, err := readTemplateFile(filePath)
		if err != nil {
			logger.Error("failed to read template file, skipping",
				zap.String("name", name),
				zap.String("file", filePath),
				zap.Error(err),
			)
			result.Failures = append(result.Failures, TemplateLoadFailure{
				Name:  name,
				Error: err.Error(),
			})
			continue
		}

		// 2e. Validate UTF-8 encoding
		if !utf8.Valid(content) {
			logger.Error("template file is not valid UTF-8, skipping",
				zap.String("name", name),
				zap.String("file", filePath),
			)
			result.Failures = append(result.Failures, TemplateLoadFailure{
				Name:  name,
				Error: fmt.Sprintf("file %q is not valid UTF-8", filePath),
			})
			continue
		}

		// 2f. Parse with text/template
		tmpl, err := parseTemplate(name, string(content), sharedTmpl, funcMap)
		if err != nil {
			logger.Error("failed to parse template, skipping",
				zap.String("name", name),
				zap.String("file", filePath),
				zap.Error(err),
			)
			result.Failures = append(result.Failures, TemplateLoadFailure{
				Name:  name,
				Error: fmt.Sprintf("parse error: %v", err),
			})
			continue
		}

		// 2g. Pre-compile parameter schemas
		schemas, err := compileParamSchemas(tc.Parameters)
		if err != nil {
			logger.Error("failed to compile parameter schemas, skipping",
				zap.String("name", name),
				zap.Error(err),
			)
			result.Failures = append(result.Failures, TemplateLoadFailure{
				Name:  name,
				Error: fmt.Sprintf("schema compilation error: %v", err),
			})
			continue
		}

		// 2h. Compute SHA-256 hash
		hash := sha256Hex(content)

		// 2i. Determine CacheEnabled (default true if nil)
		cacheEnabled := true
		if tc.CacheEnabled != nil {
			cacheEnabled = *tc.CacheEnabled
		}

		// 2j. Determine CountEnabled (default true if nil)
		countEnabled := true
		if tc.CountEnabled != nil {
			countEnabled = *tc.CountEnabled
		}

		// 2k. Build RegisteredTemplate
		var cacheTTL *time.Duration
		if tc.CacheTTL != nil {
			ttl := *tc.CacheTTL
			cacheTTL = &ttl
		}

		reg := &RegisteredTemplate{
			Name:         name,
			Description:  tc.Description,
			Config:       tc,
			Template:     tmpl,
			ParamSchemas: schemas,
			CacheEnabled: cacheEnabled,
			CacheTTL:     cacheTTL,
			CountEnabled: countEnabled,
		}

		result.Registered[name] = reg
		result.Hashes[name] = hash
	}

	// --- Step 3: Log summary ---
	names := make([]string, 0, len(result.Registered))
	for n := range result.Registered {
		names = append(names, n)
	}

	logger.Info("template loading complete",
		zap.Int("success_count", len(result.Registered)),
		zap.Int("failure_count", len(result.Failures)),
		zap.Strings("registered_templates", names),
	)

	return result, nil
}

// loadSharedFragments walks shared_dir and parses all .sql.tmpl files as
// template fragments that can be referenced via {{template "name" .}}.
func loadSharedFragments(sharedDir string, funcMap template.FuncMap, logger *zap.Logger) (*template.Template, error) {
	shared := template.New("__shared__").Funcs(funcMap).Option("missingkey=error")

	info, err := os.Stat(sharedDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("shared_dir does not exist, no shared fragments loaded",
				zap.String("shared_dir", sharedDir),
			)
			return shared, nil
		}
		return nil, fmt.Errorf("stat shared_dir %q: %w", sharedDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("shared_dir %q is not a directory", sharedDir)
	}

	err = filepath.WalkDir(sharedDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			logger.Warn("error walking shared_dir",
				zap.String("path", path),
				zap.Error(walkErr),
			)
			return nil // continue walking
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".sql.tmpl") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			logger.Error("failed to read shared fragment, skipping",
				zap.String("path", path),
				zap.Error(readErr),
			)
			return nil
		}

		if int64(len(content)) > maxTemplateFileSize {
			logger.Error("shared fragment exceeds max file size, skipping",
				zap.String("path", path),
				zap.Int("size", len(content)),
				zap.Int64("max", maxTemplateFileSize),
			)
			return nil
		}

		if !utf8.Valid(content) {
			logger.Error("shared fragment is not valid UTF-8, skipping",
				zap.String("path", path),
			)
			return nil
		}

		_, parseErr := shared.Parse(string(content))
		if parseErr != nil {
			logger.Error("failed to parse shared fragment, skipping",
				zap.String("path", path),
				zap.Error(parseErr),
			)
			return nil
		}

		logger.Debug("loaded shared fragment",
			zap.String("path", path),
		)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk shared_dir %q: %w", sharedDir, err)
	}

	return shared, nil
}

// readTemplateFile reads a template file and validates its size.
func readTemplateFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("template file %q does not exist", path)
		}
		return nil, fmt.Errorf("stat template file %q: %w", path, err)
	}

	if info.Size() > maxTemplateFileSize {
		return nil, fmt.Errorf("template file %q size %d exceeds max %d bytes", path, info.Size(), maxTemplateFileSize)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template file %q: %w", path, err)
	}

	return content, nil
}

// parseTemplate creates a new template with the given name, clones shared
// fragments into it, and parses the content.
func parseTemplate(name, content string, shared *template.Template, funcMap template.FuncMap) (*template.Template, error) {
	// Clone shared fragments so each template gets its own copy
	tmpl, err := shared.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone shared fragments: %w", err)
	}

	// Create a new template with the given name within the cloned set
	t := tmpl.New(name).Funcs(funcMap).Option("missingkey=error")

	_, err = t.Parse(content)
	if err != nil {
		return nil, err
	}

	return t, nil
}

// compileParamSchemas converts config-level TemplateParamConfig entries into
// runtime ParamSchema structs with pre-compiled regexps and typed defaults.
func compileParamSchemas(params []config.TemplateParamConfig) ([]ParamSchema, error) {
	schemas := make([]ParamSchema, 0, len(params))

	for _, p := range params {
		schema := ParamSchema{
			Name:     p.Name,
			Type:     p.Type,
			Required: p.Required,
			Enum:     p.Enum,
		}

		// MaxLength default to 1024 if not specified
		if p.MaxLength != nil {
			schema.MaxLength = *p.MaxLength
		} else {
			schema.MaxLength = 1024
		}

		// MaxItems default to 1000 if not specified
		if p.MaxItems != nil {
			schema.MaxItems = *p.MaxItems
		} else {
			schema.MaxItems = 1000
		}

		// Compile Pattern regex if specified
		if p.Pattern != nil && *p.Pattern != "" {
			re, err := regexp.Compile(*p.Pattern)
			if err != nil {
				return nil, fmt.Errorf("parameter %q: invalid pattern %q: %w", p.Name, *p.Pattern, err)
			}
			schema.Pattern = re
		}

		// Parse Default value to typed value based on Type
		if p.Default != nil {
			def, err := parseDefaultValue(*p.Default, p.Type)
			if err != nil {
				return nil, fmt.Errorf("parameter %q: invalid default %q for type %q: %w", p.Name, *p.Default, p.Type, err)
			}
			schema.Default = def
		}

		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// parseDefaultValue converts a string default value to the appropriate Go type.
func parseDefaultValue(raw string, paramType string) (any, error) {
	switch paramType {
	case "string":
		return raw, nil
	case "int":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %w", raw, err)
		}
		return v, nil
	case "float":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as float: %w", raw, err)
		}
		return v, nil
	case "boolean":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as boolean: %w", raw, err)
		}
		return v, nil
	case "string[]":
		// Default values for string[] are not supported
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported type %q", paramType)
	}
}

// sha256Hex computes the SHA-256 hash of data and returns the hex-encoded string.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
