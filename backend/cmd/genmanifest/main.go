// genmanifest 生成插件 plugin.yaml 清单文件，用于 marketplace 与 release 流程。
//
// 用法：
//
//	cd backend && go run ./cmd/genmanifest
//
// 产物位置：仓库根目录 plugin.yaml
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"

	sdk "github.com/DouDOU-start/airgate-sdk"

	"github.com/DouDOU-start/airgate-health/backend/internal/health"
)

const generatedComment = "# 本文件由 backend/cmd/genmanifest 自动生成，请勿手工修改。\n\n"

type manifest struct {
	ID             string         `yaml:"id"`
	Name           string         `yaml:"name"`
	Version        string         `yaml:"version"`
	Description    string         `yaml:"description"`
	Author         string         `yaml:"author"`
	Type           string         `yaml:"type"`
	MinCoreVersion string         `yaml:"min_core_version"`
	Dependencies   []string       `yaml:"dependencies"`
	Config         []configField  `yaml:"config,omitempty"`
	FrontendPages  []frontendPage `yaml:"frontend_pages,omitempty"`
}

type configField struct {
	Key         string `yaml:"key"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
	Placeholder string `yaml:"placeholder,omitempty"`
}

type frontendPage struct {
	Path        string `yaml:"path"`
	Title       string `yaml:"title"`
	Icon        string `yaml:"icon,omitempty"`
	Description string `yaml:"description,omitempty"`
	Audience    string `yaml:"audience,omitempty"`
}

func main() {
	content, err := renderManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成 manifest 失败: %v\n", err)
		os.Exit(1)
	}
	target, err := manifestPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "定位 plugin.yaml 失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 plugin.yaml 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已生成 %s\n", target)
}

func renderManifest() ([]byte, error) {
	info := health.BuildPluginInfo()
	doc := manifest{
		ID:             info.ID,
		Name:           info.Name,
		Version:        info.Version,
		Description:    info.Description,
		Author:         info.Author,
		Type:           string(info.Type),
		MinCoreVersion: "1.0.0",
		Dependencies:   []string{},
		Config:         convertConfig(info.ConfigSchema),
		FrontendPages:  convertPages(info.FrontendPages),
	}
	var body bytes.Buffer
	enc := yaml.NewEncoder(&body)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return append([]byte(generatedComment), body.Bytes()...), nil
}

func manifestPath() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("无法定位源文件")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	return filepath.Join(repoRoot, "plugin.yaml"), nil
}

func convertConfig(in []sdk.ConfigField) []configField {
	out := make([]configField, 0, len(in))
	for _, f := range in {
		out = append(out, configField{
			Key:         f.Key,
			Type:        f.Type,
			Required:    f.Required,
			Default:     f.Default,
			Description: f.Description,
			Placeholder: f.Placeholder,
		})
	}
	return out
}

func convertPages(in []sdk.FrontendPage) []frontendPage {
	out := make([]frontendPage, 0, len(in))
	for _, p := range in {
		out = append(out, frontendPage{
			Path:        p.Path,
			Title:       p.Title,
			Icon:        p.Icon,
			Description: p.Description,
			Audience:    p.Audience,
		})
	}
	return out
}
