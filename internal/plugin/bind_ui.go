package plugin

import (
	"fmt"
	"io/fs"
	"sort"

	"silo/internal/log"

	"github.com/dop251/goja"
)

// createUIModule собирает объект ts.ui: страницы интерфейса плагина.
func (rt *JSRuntime) createUIModule() *goja.Object {
	obj := rt.vm.NewObject()

	// ts.ui.page(id, render|options): объявляет страницу интерфейса.
	// render - функция (req) => схема, либо объект с полями title,
	// title_key, icon, rank, schema, render.
	obj.Set("page", func(call goja.FunctionCall) goja.Value {
		id := call.Argument(0).String()
		if !ValidUIPageID(id) {
			log.Warnf("[Plugin:%s] Invalid ui page id %q", rt.pluginID, id)
			return goja.Undefined()
		}

		page := &uiPage{info: UIPageInfo{ID: id}}
		arg := call.Argument(1)

		if fn, ok := goja.AssertFunction(arg); ok {
			page.render = rt.uiRender(fn)
		} else if arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			opts, ok := arg.Export().(map[string]any)
			if !ok {
				log.Warnf("[Plugin:%s] ui page %q: expected a function or object", rt.pluginID, id)
				return goja.Undefined()
			}
			page.info.Title, _ = opts["title"].(string)
			page.info.TitleKey, _ = opts["title_key"].(string)
			page.info.Icon, _ = opts["icon"].(string)
			if rank, ok := uiInt(opts["rank"]); ok {
				page.info.Rank = rank
			}
			page.schema, _ = opts["schema"].(string)
			if raw, ok := opts["render"]; ok {
				if fn, ok := goja.AssertFunction(rt.vm.ToValue(raw)); ok {
					page.render = rt.uiRender(fn)
				}
			}
		} else {
			log.Warnf("[Plugin:%s] ui page %q: renderer is required", rt.pluginID, id)
			return goja.Undefined()
		}

		if page.schema != "" && !fs.ValidPath(page.schema) {
			log.Warnf("[Plugin:%s] ui page %q: invalid schema path %q", rt.pluginID, id, page.schema)
			return goja.Undefined()
		}
		if page.render == nil && page.schema == "" {
			log.Warnf("[Plugin:%s] ui page %q: no renderer and no schema", rt.pluginID, id)
			return goja.Undefined()
		}

		if _, exists := rt.uiPages[id]; !exists && len(rt.uiPages) >= maxUIPages {
			log.Warnf("[Plugin:%s] Too many ui pages (max %d)", rt.pluginID, maxUIPages)
			return goja.Undefined()
		}

		rt.uiPages[id] = page
		log.Debugf("[Plugin:%s] Registered ui page %q", rt.pluginID, id)
		return goja.Undefined()
	})

	return obj
}

// uiRender оборачивает JS-функцию в провайдер схемы.
func (rt *JSRuntime) uiRender(fn goja.Callable) uiRenderFunc {
	return func(ctx UIContext) (any, error) {
		return rt.callUIRender(fn, ctx)
	}
}

// callUIRender вызывает JS-провайдер и возвращает схему.
// Вызывается под заблокированным мьютексом рантайма.
func (rt *JSRuntime) callUIRender(fn goja.Callable, ctx UIContext) (any, error) {
	req := rt.vm.NewObject()
	req.Set("plugin_id", rt.pluginID)
	req.Set("page", ctx.Page)
	req.Set("lang", ctx.Lang)

	query := rt.vm.NewObject()
	for k, v := range ctx.Query {
		query.Set(k, v)
	}
	req.Set("query", query)

	if ctx.User != nil {
		u := rt.vm.NewObject()
		u.Set("id", ctx.User.ID)
		u.Set("username", ctx.User.Username)
		u.Set("rank", int(ctx.User.Rank))
		u.Set("is_banned", ctx.User.IsBanned)
		req.Set("user", u)
	} else {
		req.Set("user", goja.Null())
	}

	v, err := fn(goja.Undefined(), req)
	if err != nil {
		return nil, fmt.Errorf("ui page %q: %w", ctx.Page, err)
	}
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, fmt.Errorf("ui page %q returned no schema", ctx.Page)
	}
	return v.Export(), nil
}

// RegisterUIPage заносит статическую страницу из манифеста.
func (rt *JSRuntime) RegisterUIPage(info UIPageInfo, schema string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.uiPages[info.ID] = &uiPage{info: info, schema: schema}
}

// UIPages возвращает зарегистрированные страницы.
func (rt *JSRuntime) UIPages() []UIPageInfo {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	out := make([]UIPageInfo, 0, len(rt.uiPages))
	for _, p := range rt.uiPages {
		out = append(out, p.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasUIPage сообщает, зарегистрирована ли страница.
func (rt *JSRuntime) HasUIPage(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	_, ok := rt.uiPages[id]
	return ok
}

// HasAnyUIPage сообщает, есть ли у плагина страницы интерфейса.
func (rt *JSRuntime) HasAnyUIPage() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	return len(rt.uiPages) > 0
}

// UISchema возвращает схему страницы и проверяет её.
func (rt *JSRuntime) UISchema(pageID string, ctx UIContext) (out any, err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("ui page %q panicked: %v", pageID, r)
		}
	}()

	page, ok := rt.uiPages[pageID]
	if !ok {
		return nil, ErrUIPageNotFound
	}

	if page.render != nil {
		return page.render(ctx)
	}
	if page.schema != "" {
		return uiSchemaPage(rt.vfs, page.schema)
	}
	return nil, ErrUIPageNotFound
}
