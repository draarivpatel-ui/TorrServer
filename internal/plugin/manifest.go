package plugin

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// pluginIDRe задаёт допустимые идентификаторы плагинов: буквы, цифры, точка,
// дефис, подчёркивание. ID попадает в пути на диске, поэтому разделители
// пути и ".." в него не пропускаем.
var pluginIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// uiPageIDRe задаёт допустимые идентификаторы страниц интерфейса.
var uiPageIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidPluginID проверяет, что идентификатор пригоден для имени каталога.
func ValidPluginID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return pluginIDRe.MatchString(id)
}

// ValidUIPageID проверяет идентификатор страницы интерфейса.
func ValidUIPageID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return uiPageIDRe.MatchString(id)
}

// MenuEntry - пункт меню, который плагин хочет добавить в веб-интерфейс
type MenuEntry struct {
	Title    string `yaml:"title" json:"title"`                   // Текст по умолчанию (fallback)
	TitleKey string `yaml:"title_key" json:"title_key,omitempty"` // Опциональный ключ i18n
	Route    string `yaml:"route" json:"route"`                   // Собственный роут плагина
	Page     string `yaml:"page" json:"page,omitempty"`           // Страница интерфейса вместо роута
	Rank     int    `yaml:"rank" json:"rank,omitempty"`           // 0 или отсутствие = видно всем
	Icon     string `yaml:"icon" json:"icon,omitempty"`           // Роут на иконку внутри статики плагина
}

// UIPage - страница интерфейса, объявленная в манифесте.
type UIPage struct {
	ID       string `yaml:"id" json:"id"`
	Title    string `yaml:"title" json:"title,omitempty"`
	TitleKey string `yaml:"title_key" json:"title_key,omitempty"`
	Rank     int    `yaml:"rank" json:"rank,omitempty"`
	Icon     string `yaml:"icon" json:"icon,omitempty"`
	Schema   string `yaml:"schema" json:"schema,omitempty"` // Путь к статической схеме внутри VFS
}

// UI - описание интерфейса плагина.
type UI struct {
	Version int      `yaml:"version" json:"version"`
	Pages   []UIPage `yaml:"pages" json:"pages,omitempty"`
}

type Manifest struct {
	ID          string      `yaml:"id" json:"id"`
	Name        string      `yaml:"name" json:"name"`
	Version     string      `yaml:"version" json:"version"`
	Author      string      `yaml:"author" json:"author"`
	Description string      `yaml:"description" json:"description"`
	Entry       string      `yaml:"entry" json:"entry"`
	ThemeUI     bool        `yaml:"theme_ui" json:"theme_ui"`
	Icon        string      `yaml:"icon" json:"icon,omitempty"` // Роут на иконку плагина (например /img/logo.png)
	Menu        []MenuEntry `yaml:"menu" json:"menu,omitempty"` // Пункты меню для веб-интерфейса
	Events      []string    `yaml:"events" json:"events"`       // Разрешенные внешние топики шины
	Routes      []string    `yaml:"routes" json:"routes"`       // Разрешенные веб-маршруты
	UI          *UI         `yaml:"ui" json:"ui,omitempty"`     // Страницы интерфейса
}

func LoadManifestFromFS(vfs fs.FS) (*Manifest, error) {
	data, err := fs.ReadFile(vfs, "info.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read info.yaml: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse info.yaml: %w", err)
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

// Validate проверяет обязательные поля манифеста и описание интерфейса.
func (m *Manifest) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("plugin manifest missing 'id' field")
	}
	if !ValidPluginID(m.ID) {
		return fmt.Errorf("plugin manifest has invalid 'id' %q: allowed letters, digits, dot, dash and underscore", m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("plugin manifest missing 'name' field")
	}
	return m.validateUI()
}

// validateUI проверяет версию и страницы интерфейса.
func (m *Manifest) validateUI() error {
	if m.UI == nil {
		return nil
	}

	if m.UI.Version == 0 {
		m.UI.Version = UIVersion
	}
	if m.UI.Version != UIVersion {
		return fmt.Errorf("unsupported ui version %d", m.UI.Version)
	}
	if len(m.UI.Pages) > maxUIPages {
		return fmt.Errorf("too many ui pages: %d (max %d)", len(m.UI.Pages), maxUIPages)
	}

	seen := make(map[string]bool, len(m.UI.Pages))
	for _, p := range m.UI.Pages {
		if !ValidUIPageID(p.ID) {
			return fmt.Errorf("invalid ui page id %q", p.ID)
		}
		if seen[p.ID] {
			return fmt.Errorf("duplicate ui page id %q", p.ID)
		}
		seen[p.ID] = true

		if p.Schema == "" {
			continue
		}
		if strings.Contains(p.Schema, "\\") || !fs.ValidPath(p.Schema) {
			return fmt.Errorf("invalid schema path %q for ui page %q", p.Schema, p.ID)
		}
	}

	for _, e := range m.Menu {
		if e.Page != "" && !ValidUIPageID(e.Page) {
			return fmt.Errorf("invalid ui page id %q in menu entry", e.Page)
		}
	}

	return nil
}
