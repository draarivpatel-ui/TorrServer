package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"path"
	"strings"

	"silo/internal/user"
)

const (
	// UIVersion - версия контракта интерфейса.
	UIVersion = 1
	// UIMediaType - тип содержимого схемы интерфейса.
	UIMediaType = "application/vnd.silo.ui+json"

	maxUIPages = 32
	maxUIDepth = 32
	maxUINodes = 10000
	maxUISize  = 1 << 20
)

var (
	// ErrUIPageNotFound - у плагина нет такой страницы.
	ErrUIPageNotFound = errors.New("ui page not found")
	// ErrUIPageForbidden - у пользователя нет доступа к странице.
	ErrUIPageForbidden = errors.New("ui page forbidden")
	// ErrUISchemaInvalid - плагин вернул документ, нарушающий контракт.
	ErrUISchemaInvalid = errors.New("ui schema is invalid")
)

// UIPageInfo - сведения о странице интерфейса для меню и админки.
type UIPageInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	TitleKey string `json:"title_key,omitempty"`
	Rank     int    `json:"rank"`
	Icon     string `json:"icon,omitempty"`
}

// UIContext - запрос страницы интерфейса.
type UIContext struct {
	User  *user.User
	Page  string
	Lang  string
	Query map[string]string
}

// uiRenderFunc возвращает схему страницы.
type uiRenderFunc func(ctx UIContext) (any, error)

// uiPage - зарегистрированная страница плагина.
type uiPage struct {
	info   UIPageInfo
	schema string
	render uiRenderFunc
}

// knownUIComponents - типы узлов версии 1.
var knownUIComponents = map[string]bool{
	"screen":     true,
	"section":    true,
	"form":       true,
	"row":        true,
	"column":     true,
	"divider":    true,
	"spacer":     true,
	"modal":      true,
	"text":       true,
	"link":       true,
	"notice":     true,
	"badge":      true,
	"progress":   true,
	"media":      true,
	"cards":      true,
	"table":      true,
	"list":       true,
	"grid":       true,
	"card":       true,
	"player":     true,
	"field":      true,
	"button":     true,
	"button_row": true,
}

// knownFieldInputs - типы полей ввода версии 1.
var knownFieldInputs = map[string]bool{
	"text": true, "password": true, "textarea": true, "number": true,
	"select": true, "checkbox": true, "switch": true, "range": true,
	"hidden": true,
}

// normalizeUISchema проверяет документ, нормализует пути и собирает
// объявленные действия.
func normalizeUISchema(pluginID string, raw any) (map[string]any, error) {
	w := &uiWalk{pluginID: pluginID, ids: make(map[string]bool)}

	doc, err := w.node(raw, 0, "root")
	if err != nil {
		return nil, err
	}

	if typ, _ := doc["type"].(string); typ != "screen" {
		return nil, fmt.Errorf("root node must be 'screen', got %q", typ)
	}
	version, ok := uiInt(doc["version"])
	if !ok || version != UIVersion {
		return nil, fmt.Errorf("unsupported ui version %v", doc["version"])
	}

	if err := w.actions(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// uiWalk обходит документ и проверяет узлы.
type uiWalk struct {
	pluginID string
	ids      map[string]bool
	nodes    int
}

// node проверяет один узел и его детей.
func (w *uiWalk) node(raw any, depth int, where string) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: node must be an object", where)
	}
	if depth > maxUIDepth {
		return nil, fmt.Errorf("%s: nesting is too deep", where)
	}
	w.nodes++
	if w.nodes > maxUINodes {
		return nil, fmt.Errorf("%s: too many nodes", where)
	}

	typ, _ := m["type"].(string)
	if typ == "" {
		return nil, fmt.Errorf("%s: missing 'type'", where)
	}
	if !knownUIComponents[typ] {
		return nil, fmt.Errorf("%s: unknown component %q", where, typ)
	}

	if rawID, ok := m["id"]; ok && rawID != nil {
		id, ok := rawID.(string)
		if !ok || !ValidUIPageID(id) {
			return nil, fmt.Errorf("%s: invalid id", where)
		}
		if w.ids[id] {
			return nil, fmt.Errorf("%s: duplicate id %q", where, id)
		}
		w.ids[id] = true
	}

	if rawProps, ok := m["props"]; ok && rawProps != nil {
		props, ok := rawProps.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: props must be an object", where)
		}
		if err := w.props(typ, m, props, depth, where); err != nil {
			return nil, err
		}
	} else if typ == "field" {
		return nil, fmt.Errorf("%s: 'field' requires props", where)
	}
	if typ == "modal" {
		if id, _ := m["id"].(string); id == "" {
			return nil, fmt.Errorf("%s: modal requires id", where)
		}
	}

	if rawChildren, ok := m["children"]; ok && rawChildren != nil {
		list, ok := rawChildren.([]any)
		if !ok {
			return nil, fmt.Errorf("%s: children must be an array", where)
		}
		out := make([]any, 0, len(list))
		for i, child := range list {
			n, err := w.node(child, depth+1, fmt.Sprintf("%s.children[%d]", where, i))
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		m["children"] = out
	}

	return m, nil
}

// props проверяет свойства узла и нормализует адреса.
func (w *uiWalk) props(typ string, node, props map[string]any, depth int, where string) error {
	if icon, ok := props["icon"].(string); ok && icon != "" {
		norm, err := normalizeUIURL(w.pluginID, icon, true)
		if err != nil {
			return fmt.Errorf("%s: icon: %w", where, err)
		}
		props["icon"] = norm
	}

	switch typ {
	case "field":
		id, _ := node["id"].(string)
		if id == "" {
			return fmt.Errorf("%s: field requires id", where)
		}
		input, _ := props["input"].(string)
		if !knownFieldInputs[input] {
			return fmt.Errorf("%s: unknown field input %q", where, input)
		}
		if raw, ok := props["options"]; ok && raw != nil {
			options, ok := raw.([]any)
			if !ok {
				return fmt.Errorf("%s: options must be an array", where)
			}
			for i, o := range options {
				if _, ok := o.(map[string]any); !ok {
					return fmt.Errorf("%s: options[%d] must be an object", where, i)
				}
			}
		}
		if raw, ok := props["rows"]; ok && raw != nil {
			rows, ok := uiInt(raw)
			if !ok || rows < 1 || rows > 200 {
				return fmt.Errorf("%s: rows must be between 1 and 200", where)
			}
			props["rows"] = rows
		}

	case "link":
		href, _ := props["href"].(string)
		norm, err := normalizeUIURL(w.pluginID, href, true)
		if err != nil {
			return fmt.Errorf("%s: href: %w", where, err)
		}
		props["href"] = norm

	case "media":
		src, _ := props["src"].(string)
		norm, err := normalizeUIURL(w.pluginID, src, true)
		if err != nil {
			return fmt.Errorf("%s: src: %w", where, err)
		}
		props["src"] = norm

	case "table":
		if err := w.columns(props, where); err != nil {
			return err
		}
		if raw, ok := props["rows"]; ok && raw != nil {
			rows, ok := raw.([]any)
			if !ok {
				return fmt.Errorf("%s: rows must be an array", where)
			}
			for i, r := range rows {
				if _, ok := r.(map[string]any); !ok {
					return fmt.Errorf("%s: rows[%d] must be an object", where, i)
				}
			}
		}
		if err := w.sourceAndRefresh(props, where); err != nil {
			return err
		}
		if err := w.actionList(props["row_actions"], where+".row_actions"); err != nil {
			return err
		}

	case "list":
		if raw, ok := props["items"]; ok && raw != nil {
			if _, ok := raw.([]any); !ok {
				return fmt.Errorf("%s: items must be an array", where)
			}
		}
		if err := w.sourceAndRefresh(props, where); err != nil {
			return err
		}

	case "grid":
		if raw, ok := props["items"]; ok && raw != nil {
			items, ok := raw.([]any)
			if !ok {
				return fmt.Errorf("%s: items must be an array", where)
			}
			out := make([]any, 0, len(items))
			for i, item := range items {
				n, err := w.node(item, depth+1, fmt.Sprintf("%s.items[%d]", where, i))
				if err != nil {
					return err
				}
				out = append(out, n)
			}
			props["items"] = out
		}
		if err := w.sourceAndRefresh(props, where); err != nil {
			return err
		}

	case "card":
		if raw, ok := props["poster"].(string); ok && raw != "" {
			norm, err := normalizeUIURL(w.pluginID, raw, true)
			if err != nil {
				return fmt.Errorf("%s: poster: %w", where, err)
			}
			props["poster"] = norm
		}
		if raw, ok := props["meta"]; ok && raw != nil {
			meta, ok := raw.([]any)
			if !ok {
				return fmt.Errorf("%s: meta must be an array", where)
			}
			for i, m := range meta {
				item, ok := m.(map[string]any)
				if !ok {
					return fmt.Errorf("%s: meta[%d] must be an object", where, i)
				}
				if icon, ok := item["icon"].(string); ok && icon != "" {
					norm, err := normalizeUIURL(w.pluginID, icon, true)
					if err != nil {
						return fmt.Errorf("%s: meta[%d].icon: %w", where, i, err)
					}
					item["icon"] = norm
				}
			}
		}
		if err := w.actionList(props["menu"], where+".menu"); err != nil {
			return err
		}
	}

	return nil
}

// columns проверяет описание колонок таблицы.
func (w *uiWalk) columns(props map[string]any, where string) error {
	raw, ok := props["columns"]
	if !ok || raw == nil {
		return fmt.Errorf("%s: columns are required", where)
	}
	columns, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s: columns must be an array", where)
	}
	for i, c := range columns {
		col, ok := c.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: columns[%d] must be an object", where, i)
		}
		if id, _ := col["id"].(string); id == "" {
			return fmt.Errorf("%s: columns[%d] requires id", where, i)
		}
	}
	return nil
}

// actionList проверяет список действий строки или карточки.
func (w *uiWalk) actionList(raw any, where string) error {
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", where)
	}
	for i, a := range list {
		item, ok := a.(map[string]any)
		if !ok {
			return fmt.Errorf("%s[%d] must be an object", where, i)
		}
		if name, _ := item["action"].(string); name == "" {
			return fmt.Errorf("%s[%d] requires action", where, i)
		}
		if icon, ok := item["icon"].(string); ok && icon != "" {
			norm, err := normalizeUIURL(w.pluginID, icon, true)
			if err != nil {
				return fmt.Errorf("%s[%d].icon: %w", where, i, err)
			}
			item["icon"] = norm
		}
	}
	return nil
}

// sourceAndRefresh проверяет источник данных и период обновления.
func (w *uiWalk) sourceAndRefresh(props map[string]any, where string) error {
	if raw, ok := props["source"]; ok && raw != nil {
		src, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: source must be an object", where)
		}
		method, _ := src["method"].(string)
		method = strings.ToUpper(method)
		if method != "GET" && method != "POST" {
			return fmt.Errorf("%s: source method must be GET or POST", where)
		}
		src["method"] = method
		p, _ := src["path"].(string)
		norm, err := normalizeActionPath(w.pluginID, p)
		if err != nil {
			return fmt.Errorf("%s: source path: %w", where, err)
		}
		src["path"] = norm
		if q, ok := src["query"]; ok && q != nil {
			if _, ok := q.(map[string]any); !ok {
				return fmt.Errorf("%s: source query must be an object", where)
			}
		}
	}

	if raw, ok := props["refresh"]; ok && raw != nil {
		ref, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: refresh must be an object", where)
		}
		interval, ok := uiInt(ref["interval"])
		if !ok || interval < 1000 {
			return fmt.Errorf("%s: refresh interval must be at least 1000 ms", where)
		}
		ref["interval"] = interval
	}
	return nil
}

// actions проверяет действия страницы и нормализует их адреса.
func (w *uiWalk) actions(root map[string]any) error {
	raw, ok := root["actions"]
	if !ok || raw == nil {
		return nil
	}
	actions, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("actions must be an object")
	}

	for name, a := range actions {
		if !ValidUIPageID(name) {
			return fmt.Errorf("invalid action name %q", name)
		}
		action, ok := a.(map[string]any)
		if !ok {
			return fmt.Errorf("action %q must be an object", name)
		}

		typ, _ := action["type"].(string)
		switch typ {
		case "submit":
			method, _ := action["method"].(string)
			method = strings.ToUpper(method)
			switch method {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
			default:
				return fmt.Errorf("action %q: unsupported method %q", name, method)
			}
			action["method"] = method

			p, _ := action["path"].(string)
			norm, err := normalizeActionPath(w.pluginID, p)
			if err != nil {
				return fmt.Errorf("action %q: %w", name, err)
			}
			action["path"] = norm

			if body, ok := action["body"]; ok && body != nil {
				if err := validateJSONValue(body, 0); err != nil {
					return fmt.Errorf("action %q: body: %w", name, err)
				}
			}
			if err := validateJSONObject(action["query"], name); err != nil {
				return err
			}

		case "set":
			values, ok := action["values"].(map[string]any)
			if !ok {
				return fmt.Errorf("action %q: values must be an object", name)
			}
			if err := validateJSONValue(values, 0); err != nil {
				return fmt.Errorf("action %q: values: %w", name, err)
			}

		case "reload":
			// полей нет

		case "open", "close":
			modal, _ := action["modal"].(string)
			if !ValidUIPageID(modal) {
				return fmt.Errorf("action %q: invalid modal id", name)
			}

		case "navigate":
			href, _ := action["href"].(string)
			norm, err := normalizeUIURL(w.pluginID, href, true)
			if err != nil {
				return fmt.Errorf("action %q: href: %w", name, err)
			}
			action["href"] = norm

		default:
			return fmt.Errorf("action %q: unknown type %q", name, typ)
		}

		if confirm, ok := action["confirm"]; ok && confirm != nil {
			if _, ok := confirm.(map[string]any); !ok {
				return fmt.Errorf("action %q: confirm must be an object", name)
			}
		}
	}

	return nil
}

// validateJSONObject проверяет, что необязательное поле - объект.
func validateJSONObject(raw any, name string) error {
	if raw == nil {
		return nil
	}
	if _, ok := raw.(map[string]any); !ok {
		return fmt.Errorf("action %q: query must be an object", name)
	}
	return nil
}

// validateJSONValue проверяет, что значение собирается в JSON.
func validateJSONValue(v any, depth int) error {
	if depth > 16 {
		return fmt.Errorf("value is too deep")
	}
	switch x := v.(type) {
	case nil, string, bool, int, int32, int64, float32, float64, json.Number:
		return nil
	case []any:
		for _, item := range x {
			if err := validateJSONValue(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for _, item := range x {
			if err := validateJSONValue(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported value type %T", v)
	}
}

// normalizeActionPath приводит путь к /plugins/<id>/...
func normalizeActionPath(pluginID, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return "", fmt.Errorf("invalid path %q", raw)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	if u.Scheme != "" || u.Host != "" {
		return "", fmt.Errorf("absolute addresses are not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("query and fragment are not allowed in path")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("path must not contain '..'")
		}
	}

	clean := path.Clean("/" + u.Path)
	if clean == "/" || clean == "/." {
		return "", fmt.Errorf("path is required")
	}

	own := "/plugins/" + pluginID
	if clean == own || strings.HasPrefix(clean, own+"/") {
		return clean, nil
	}
	if strings.HasPrefix(clean, "/plugins/") {
		return "", fmt.Errorf("path must belong to this plugin")
	}
	return own + clean, nil
}

// normalizeUIURL проверяет адрес картинки или ссылки.
func normalizeUIURL(pluginID, raw string, allowExternal bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return "", fmt.Errorf("invalid address %q", raw)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", raw)
	}
	if u.Scheme != "" {
		if allowExternal && (u.Scheme == "http" || u.Scheme == "https") {
			return u.String(), nil
		}
		return "", fmt.Errorf("scheme %q is not allowed", u.Scheme)
	}
	if u.Host != "" {
		return "", fmt.Errorf("host is not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("query and fragment are not allowed")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("address must not contain '..'")
		}
	}

	clean := path.Clean(u.Path)
	if clean == "." {
		return "/plugins/" + pluginID + "/", nil
	}
	if strings.HasPrefix(clean, "/plugins/") {
		own := "/plugins/" + pluginID
		if clean == own || strings.HasPrefix(clean, own+"/") {
			return clean, nil
		}
		return "", fmt.Errorf("path must belong to this plugin")
	}
	if strings.HasPrefix(clean, "/") {
		return clean, nil
	}
	return "/plugins/" + pluginID + "/" + strings.TrimPrefix(clean, "./"), nil
}

// uiInt переводит число из goja или JSON в int.
func uiInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		if float32(math.Trunc(float64(n))) != n {
			return 0, false
		}
		return int(n), true
	case float64:
		if math.Trunc(n) != n {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// uiSchemaPage читает статическую схему страницы из VFS.
func uiSchemaPage(vfs fs.FS, schemaPath string) (any, error) {
	data, err := fs.ReadFile(vfs, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ui schema %q: %w", schemaPath, err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse ui schema %q: %w", schemaPath, err)
	}
	return doc, nil
}
