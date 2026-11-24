'use strict';

// Рендерер UI-схемы плагина.
// Рисует документ по контракту ui.md и выполняет объявленные действия.
(function (global) {
    const UI_MEDIA = 'application/vnd.silo.ui+json';

    // tr переводит ключ, подставляя запасной текст.
    function tr(key, fallback) {
        if (!key) return fallback == null ? '' : String(fallback);
        if (global.i18n && typeof global.i18n.t === 'function') {
            return global.i18n.t(key, fallback);
        }
        return fallback == null ? key : String(fallback);
    }

    // el создаёт узел с классом и текстом.
    function el(tag, cls, text) {
        const node = document.createElement(tag);
        if (cls) node.className = cls;
        if (text !== undefined && text !== null) node.textContent = String(text);
        return node;
    }

    // textOf берёт перевод по ключу, иначе готовый текст.
    function textOf(props, keyName, textName) {
        if (!props) return '';
        const key = props[keyName];
        const text = props[textName];
        if (key) return tr(key, text);
        if (text !== undefined && text !== null) return String(text);
        return '';
    }

    // oneOf оставляет только допустимое значение стиля.
    function oneOf(value, allowed, fallback) {
        return allowed.indexOf(value) >= 0 ? value : fallback;
    }

    // safeHref отбрасывает опасные схемы ссылок.
    function safeHref(raw) {
        if (!raw) return '';
        const value = String(raw).trim();
        if (/^(javascript|data|vbscript):/i.test(value)) return '';
        return value;
    }

    // safeSrc отбрасывает опасные схемы картинок.
    function safeSrc(raw) {
        if (!raw) return '';
        const value = String(raw).trim();
        if (/^(javascript|data|vbscript):/i.test(value)) return '';
        return value;
    }

    // absolutize делает адрес полным для показа.
    function absolutize(raw) {
        if (!raw) return '';
        const value = String(raw);
        if (/^https?:\/\//i.test(value)) return value;
        if (value.charAt(0) === '/') return location.origin + value;
        return location.origin + '/' + value.replace(/^\/+/, '');
    }

    // toast показывает всплывающее сообщение.
    function toast(message, style) {
        const root = document.getElementById('ui-toast-root');
        if (!root || !message) return;

        const node = el('div', 'toast toast-' + oneOf(style, ['info', 'success', 'error'], 'info'), message);
        root.appendChild(node);
        requestAnimationFrame(function () { node.classList.add('toast-show'); });
        setTimeout(function () {
            node.classList.remove('toast-show');
            setTimeout(function () { node.remove(); }, 300);
        }, 2500);
    }

    // copyText копирует текст и сообщает об этом.
    function copyText(text, button) {
        function done() {
            toast(tr('copied', 'Copied'), 'success');
            if (button) {
                button.disabled = false;
            }
        }
        if (button) button.disabled = true;

        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text).then(done).catch(function () {
                fallbackCopy(text);
                done();
            });
            return;
        }
        fallbackCopy(text);
        done();
    }

    // fallbackCopy копирует текст без Clipboard API.
    function fallbackCopy(text) {
        const area = document.createElement('textarea');
        area.value = text;
        area.setAttribute('readonly', '');
        area.style.position = 'fixed';
        area.style.opacity = '0';
        document.body.appendChild(area);
        area.select();
        try { document.execCommand('copy'); } catch (e) {}
        area.remove();
    }

    // confirmDialog спрашивает подтверждение и возвращает Promise.
    function confirmDialog(opts) {
        opts = opts || {};
        return new Promise(function (resolve) {
            const overlay = el('div', 'modal-overlay');
            const box = el('div', 'modal modal-sm');

            const header = el('div', 'modal-header');
            header.appendChild(el('h2', '', textOf(opts, 'title_key', 'title') || tr('confirm', 'Confirm')));
            const close = el('button', 'modal-close', 'x');
            close.type = 'button';
            header.appendChild(close);

            const body = el('div', 'modal-body');
            body.appendChild(el('p', 'hint', textOf(opts, 'text_key', 'text')));

            const footer = el('div', 'modal-footer');
            const cancel = el('button', 'btn btn-secondary', tr(opts.cancel_key || 'cancel', 'Cancel'));
            cancel.type = 'button';
            const ok = el('button', 'btn btn-danger', tr(opts.ok_key || 'ok', 'OK'));
            ok.type = 'button';
            footer.appendChild(cancel);
            footer.appendChild(ok);

            box.appendChild(header);
            box.appendChild(body);
            box.appendChild(footer);
            overlay.appendChild(box);
            document.body.appendChild(overlay);

            function done(value) {
                overlay.remove();
                document.removeEventListener('keydown', onKey);
                resolve(value);
            }
            function onKey(e) {
                if (e.key === 'Escape') done(false);
            }

            close.addEventListener('click', function () { done(false); });
            cancel.addEventListener('click', function () { done(false); });
            ok.addEventListener('click', function () { done(true); });
            overlay.addEventListener('click', function (e) {
                if (e.target === overlay) done(false);
            });
            document.addEventListener('keydown', onKey);
            ok.focus();
        });
    }

    // readField возвращает значение поля.
    function readField(input) {
        if (!input) return undefined;
        if (input.type === 'checkbox') return input.checked;
        if (input.type === 'number' || input.type === 'range') {
            if (input.value === '') return '';
            const n = Number(input.value);
            return isNaN(n) ? input.value : n;
        }
        return input.value;
    }

    // collectValues собирает значения всех полей страницы.
    function collectValues(ctx) {
        const out = {};
        Object.keys(ctx.fields).forEach(function (id) {
            const input = ctx.fields[id];
            if (input) out[id] = readField(input);
        });
        return out;
    }

    // setValues раскладывает значения по полям.
    function setValues(ctx, values) {
        if (!values) return;
        Object.keys(values).forEach(function (id) {
            const input = ctx.fields[id];
            if (!input) return;
            if (input.type === 'checkbox') {
                input.checked = !!values[id];
            } else {
                input.value = values[id] == null ? '' : String(values[id]);
            }
            ctx.values[id] = readField(input);
        });
    }

    // showErrors показывает ошибки рядом с полями.
    function showErrors(ctx, errors) {
        Object.keys(ctx.fields).forEach(function (id) {
            const box = document.getElementById('ui-err-' + id);
            if (!box) return;
            box.textContent = '';
            box.hidden = true;
        });

        Object.keys(errors || {}).forEach(function (id) {
            const box = document.getElementById('ui-err-' + id);
            if (!box) return;
            const message = errors[id];
            box.textContent = tr(message, message);
            box.hidden = false;
        });
    }

    // resolve подставляет значения полей и элемента списка.
    function resolve(value, ctx, item) {
        if (typeof value === 'string') {
            if (value.indexOf('$item.') === 0) {
                if (item == null) return undefined;
                return value.slice(6).split('.').reduce(function (acc, key) {
                    return acc == null ? undefined : acc[key];
                }, item);
            }
            if (value.charAt(0) === '$') {
                const key = value.slice(1);
                if (ctx.values[key] !== undefined) return ctx.values[key];
                if (ctx.fields[key]) return readField(ctx.fields[key]);
                return undefined;
            }
            return value;
        }
        if (Array.isArray(value)) {
            return value.map(function (v) { return resolve(v, ctx, item); });
        }
        if (value && typeof value === 'object') {
            const out = {};
            Object.keys(value).forEach(function (key) {
                out[key] = resolve(value[key], ctx, item);
            });
            return out;
        }
        return value;
    }

    // applyPatches применяет точечные изменения к документу.
    function applyPatches(ctx, patches) {
        if (!Array.isArray(patches)) return;

        let remount = false;
        let reload = false;

        patches.forEach(function (patch) {
            if (!patch || !patch.op) return;
            switch (patch.op) {
                case 'values':
                    setValues(ctx, patch.values);
                    break;
                case 'replace':
                    if (replaceNode(ctx.doc, patch.target, patch.node)) remount = true;
                    break;
                case 'remove':
                    if (removeNode(ctx.doc, patch.target)) remount = true;
                    break;
                case 'upsert':
                    if (upsertItem(ctx.doc, patch.target, patch.item)) remount = true;
                    break;
                case 'delete':
                    if (deleteItem(ctx.doc, patch.target, patch.item_id)) remount = true;
                    break;
                case 'reload':
                    reload = true;
                    break;
            }
        });

        if (reload) {
            load(ctx);
            return;
        }
        if (remount) mount(ctx, ctx.doc);
    }

    // walk обходит узлы документа, включая элементы сетки.
    function walk(node, visit) {
        if (!node || typeof node !== 'object') return false;
        if (visit(node)) return true;

        const children = node.children || [];
        for (let i = 0; i < children.length; i++) {
            if (walk(children[i], visit)) return true;
        }

        const items = (node.props && node.props.items) || [];
        for (let i = 0; i < items.length; i++) {
            if (items[i] && items[i].type && walk(items[i], visit)) return true;
        }
        return false;
    }

    // replaceNode заменяет узел по идентификатору.
    function replaceNode(doc, id, replacement) {
        if (!id || !replacement) return false;
        let done = false;
        walk(doc, function (node) {
            if (node.id !== id) return false;
            Object.keys(node).forEach(function (key) { delete node[key]; });
            Object.keys(replacement).forEach(function (key) { node[key] = replacement[key]; });
            done = true;
            return true;
        });
        return done;
    }

    // removeNode удаляет узел по идентификатору.
    function removeNode(node, id) {
        const children = node.children || [];
        for (let i = 0; i < children.length; i++) {
            if (children[i].id === id) {
                children.splice(i, 1);
                return true;
            }
            if (removeNode(children[i], id)) return true;
        }

        const items = (node.props && node.props.items) || [];
        for (let i = 0; i < items.length; i++) {
            if (items[i].id === id) {
                items.splice(i, 1);
                return true;
            }
        }
        return false;
    }

    // upsertItem обновляет элемент списка, таблицы или сетки.
    function upsertItem(doc, target, item) {
        if (!item || !item.id) return false;
        let done = false;
        walk(doc, function (node) {
            if (node.id !== target) return false;
            const props = node.props || (node.props = {});
            const key = node.type === 'table' ? 'rows' : 'items';
            const list = props[key] || (props[key] = []);
            const index = list.findIndex(function (x) { return String(x.id) === String(item.id); });
            if (index >= 0) {
                list[index] = item;
            } else {
                list.push(item);
            }
            done = true;
            return true;
        });
        return done;
    }

    // deleteItem удаляет элемент списка, таблицы или сетки.
    function deleteItem(doc, target, itemID) {
        if (!itemID) return false;
        let done = false;
        walk(doc, function (node) {
            if (node.id !== target) return false;
            const props = node.props || {};
            const key = node.type === 'table' ? 'rows' : 'items';
            const list = props[key] || [];
            const index = list.findIndex(function (x) { return String(x.id) === String(itemID); });
            if (index >= 0) {
                list.splice(index, 1);
                done = true;
            }
            return true;
        });
        return done;
    }

    // load запрашивает схему страницы.
    function load(ctx) {
        const url = ctx.base + 'ui/' + encodeURIComponent(ctx.page);
        return fetch(url, {
            headers: { 'Accept': UI_MEDIA },
            credentials: 'same-origin',
            cache: 'no-store'
        }).then(function (res) {
            if (res.status === 401) {
                location.href = '/login';
                return null;
            }
            if (!res.ok) {
                return res.json().catch(function () { return null; }).then(function (data) {
                    throw new Error(data && data.error ? data.error : res.status + ' ' + res.statusText);
                });
            }
            return res.json();
        }).then(function (doc) {
            if (doc) mount(ctx, doc);
        }).catch(function (err) {
            showFatal(ctx, err.message);
        });
    }

    // showFatal показывает ошибку вместо страницы.
    function showFatal(ctx, message) {
        ctx.root.textContent = '';
        const box = el('div', 'ui-notice ui-notice-danger');
        box.appendChild(el('strong', '', tr('error', 'Error')));
        box.appendChild(el('span', '', String(message)));
        ctx.root.appendChild(box);
    }

    // stopTimers останавливает периодические обновления.
    function stopTimers(ctx) {
        (ctx.timers || []).forEach(function (timer) { clearInterval(timer); });
        ctx.timers = [];
    }

    // mount рисует документ в корневой узел.
    function mount(ctx, doc) {
        stopTimers(ctx);
        closeModal(ctx);

        ctx.doc = doc;
        ctx.fields = {};
        ctx.values = {};
        ctx.modals = {};

        const root = ctx.root;
        root.textContent = '';

        if (!doc || doc.type !== 'screen') {
            showFatal(ctx, tr('invalid_ui', 'Invalid interface'));
            return;
        }

        const title = textOf(doc.props, 'title_key', 'title');
        const titleEl = document.getElementById('ui-title');
        if (titleEl) titleEl.textContent = title || ctx.plugin;
        document.title = (title ? title + ' - ' : '') + 'Silo';

        const back = document.getElementById('ui-back');
        if (back) back.hidden = !!(doc.props && doc.props.back === false);

        const content = renderNode(doc, ctx);
        if (content) root.appendChild(content);

        startSources(ctx);
    }

    // appendChildren рисует детей узла.
    function appendChildren(parent, node, ctx) {
        (node.children || []).forEach(function (child) {
            const out = renderNode(child, ctx);
            if (out) parent.appendChild(out);
        });
    }

    // renderNode рисует один узел.
    function renderNode(node, ctx) {
        if (!node || typeof node !== 'object') return null;
        if (node.props && node.props.hidden) return null;

        const render = registry[node.type];
        if (!render) {
            const box = document.createDocumentFragment();
            appendChildren(box, node, ctx);
            return box;
        }
        return render(node, ctx);
    }

    // renderChildren возвращает контейнер с детьми.
    function renderChildren(node, ctx, tag, cls) {
        const box = el(tag || 'div', cls || '');
        appendChildren(box, node, ctx);
        return box;
    }

    // buttonClass подбирает класс кнопки по смыслу.
    function buttonClass(style) {
        switch (style) {
            case 'primary':
                return '';
            case 'danger':
                return 'btn-danger';
            case 'secondary':
            case 'default':
            default:
                return 'btn-secondary';
        }
    }

    // badgeText возвращает текст метки.
    function badgeText(badge) {
        if (!badge) return '';
        if (badge.text_key) return tr(badge.text_key, badge.value);
        return badge.value == null ? '' : String(badge.value);
    }

    // openModal открывает модальное окно из документа.
    function openModal(ctx, id) {
        const node = ctx.modals[id];
        if (!node) {
            toast(tr('modal_not_found', 'Dialog not found'), 'error');
            return;
        }

        closeModal(ctx);

        const props = node.props || {};
        const overlay = el('div', 'modal-overlay');
        overlay.dataset.modal = id;

        const box = el('div', 'modal' + (props.size === 'lg' ? ' modal-lg' : ''));
        const header = el('div', 'modal-header');
        header.appendChild(el('h2', '', textOf(props, 'title_key', 'title')));
        const close = el('button', 'modal-close', 'x');
        close.type = 'button';
        close.addEventListener('click', function () { closeModal(ctx, id); });
        header.appendChild(close);

        const body = el('div', 'modal-body');
        const known = Object.keys(ctx.fields);
        appendChildren(body, node, ctx);
        overlay.__fields = Object.keys(ctx.fields).filter(function (key) {
            return known.indexOf(key) === -1;
        });

        box.appendChild(header);
        box.appendChild(body);
        overlay.appendChild(box);

        overlay.addEventListener('click', function (e) {
            if (e.target === overlay) closeModal(ctx, id);
        });

        ctx.modalKey = function (e) {
            if (e.key === 'Escape') closeModal(ctx, id);
        };
        document.addEventListener('keydown', ctx.modalKey);
        document.body.appendChild(overlay);
    }

    // closeModal закрывает модальное окно или все окна.
    function closeModal(ctx, id) {
        let closed = false;
        document.querySelectorAll('.modal-overlay[data-modal]').forEach(function (overlay) {
            if (id && overlay.dataset.modal !== id) return;
            (overlay.__fields || []).forEach(function (key) {
                delete ctx.fields[key];
                delete ctx.values[key];
            });
            overlay.remove();
            closed = true;
        });

        if ((!id || closed) && ctx.modalKey) {
            document.removeEventListener('keydown', ctx.modalKey);
            ctx.modalKey = null;
        }
    }

    // runAction выполняет действие по имени.
    function runAction(ctx, name, opts) {
        opts = opts || {};
        const actions = (ctx.doc && ctx.doc.actions) || {};
        const action = actions[name];

        if (!action) {
            toast(tr('unknown_action', 'Unknown action') + ': ' + name, 'error');
            return;
        }

        switch (action.type) {
            case 'set':
                applySet(ctx, action, opts);
                break;
            case 'reload':
                load(ctx);
                break;
            case 'open':
                openModal(ctx, action.modal);
                break;
            case 'close':
                closeModal(ctx, action.modal);
                break;
            case 'navigate':
                location.href = safeHref(action.href) || '/';
                break;
            case 'submit':
                submit(ctx, action, opts);
                break;
            default:
                toast(tr('unknown_action', 'Unknown action') + ': ' + action.type, 'error');
        }
    }

    // applySet заполняет поля, при необходимости спросив подтверждение.
    function applySet(ctx, action, opts) {
        const apply = function () {
            setValues(ctx, resolve(action.values, ctx, opts.item));
        };
        if (!action.confirm) {
            apply();
            return;
        }
        confirmDialog(action.confirm).then(function (ok) {
            if (ok) apply();
        });
    }

    // submit отправляет действие на endpoint плагина.
    function submit(ctx, action, opts) {
        opts = opts || {};

        const request = function () {
            let body = action.body ? resolve(action.body, ctx, opts.item) : collectValues(ctx);
            if (opts.params) {
                const extra = resolve(opts.params, ctx, opts.item);
                if (extra && typeof extra === 'object') {
                    body = Object.assign({}, body, extra);
                }
            }

            let url = action.path;
            const method = (action.method || 'POST').toUpperCase();
            if (action.query) {
                const query = resolve(action.query, ctx, opts.item);
                const params = new URLSearchParams();
                Object.keys(query || {}).forEach(function (key) {
                    if (query[key] !== undefined && query[key] !== null) {
                        params.set(key, String(query[key]));
                    }
                });
                const suffix = params.toString();
                if (suffix) url += (url.indexOf('?') >= 0 ? '&' : '?') + suffix;
            }

            const button = opts.button;
            if (button) button.disabled = true;

            const init = {
                method: method,
                credentials: 'same-origin',
                headers: { 'Accept': 'application/json' }
            };
            if (method !== 'GET' && method !== 'HEAD') {
                init.headers['Content-Type'] = 'application/json';
                init.body = JSON.stringify(body);
            }

            fetch(url, init).then(function (res) {
                if (res.status === 401) {
                    location.href = '/login';
                    return null;
                }
                return res.json().catch(function () { return null; }).then(function (data) {
                    if (!res.ok && !(data && (data.errors || data.error))) {
                        toast(tr('error', 'Error') + ': ' + res.status, 'error');
                        return;
                    }
                    handleResponse(ctx, data, action, res);
                });
            }).catch(function (err) {
                toast(tr('error', 'Error') + ': ' + err.message, 'error');
            }).finally(function () {
                if (button) button.disabled = false;
            });
        };

        if (!action.confirm) {
            request();
            return;
        }
        confirmDialog(action.confirm).then(function (ok) {
            if (ok) request();
        });
    }

    // handleResponse применяет ответ плагина.
    function handleResponse(ctx, data, action, res) {
        if (!data || typeof data !== 'object') {
            if (res && res.ok && action && action.success_key) {
                toast(tr(action.success_key), 'success');
            }
            return;
        }

        const failed = data.ok === false || (data.status && data.status !== 'ok');
        if (data.errors) showErrors(ctx, data.errors);
        if (data.values) setValues(ctx, data.values);
        if (data.schema) {
            mount(ctx, data.schema);
            return;
        }
        if (data.patch) applyPatches(ctx, data.patch);
        if (data.toast) {
            toast(tr(data.toast.text_key, data.toast.text || data.toast.value), data.toast.style || 'info');
        }
        if (data.redirect) {
            location.href = safeHref(data.redirect) || '/';
            return;
        }

        if (failed) {
            const message = data.error_key ? tr(data.error_key) :
                (data.error ? tr(data.error, data.error) : '');
            toast(message || (action && action.error_key ? tr(action.error_key) : tr('error', 'Error')), 'error');
            return;
        }
        if (!data.errors && action && action.success_key) {
            toast(tr(action.success_key), 'success');
        }
        if (action && action.reload) {
            load(ctx);
        }
    }

    // startSources запускает обновление блоков с источником.
    function startSources(ctx) {
        walk(ctx.doc, function (node) {
            const props = node.props || {};
            if (!props.source || !props.refresh) return false;
            const interval = Number(props.refresh.interval);
            if (!interval || interval < 1000) return false;

            const timer = setInterval(function () { refreshSource(ctx, node); }, interval);
            ctx.timers.push(timer);
            return false;
        });
    }

    // refreshSource обновляет данные блока из его источника.
    function refreshSource(ctx, node) {
        const props = node.props || {};
        if (!props.source || !props.source.path) return;

        const method = (props.source.method || 'GET').toUpperCase();
        fetch(props.source.path, {
            method: method,
            credentials: 'same-origin',
            headers: { 'Accept': 'application/json' }
        }).then(function (res) {
            return res.ok ? res.json() : null;
        }).then(function (data) {
            if (!data) return;
            if (data.rows && node.type === 'table') {
                node.props.rows = data.rows;
            } else if (data.items) {
                node.props.items = data.items;
            } else if (data.node) {
                replaceNode(ctx.doc, node.id, data.node);
            } else {
                return;
            }
            mount(ctx, ctx.doc);
        }).catch(function () {});
    }

    // registry связывает тип узла с его отрисовкой.
    const registry = {
        screen: function (node, ctx) {
            return renderChildren(node, ctx, 'div', 'ui-screen');
        },

        section: function (node, ctx) {
            const props = node.props || {};
            if (props.legend_key || props.legend) {
                const box = el('fieldset', 'subblock');
                box.appendChild(el('legend', '', textOf(props, 'legend_key', 'legend')));
                appendChildren(box, node, ctx);
                return box;
            }

            const box = el('section', 'panel ui-section');
            const title = textOf(props, 'title_key', 'title');
            if (title) box.appendChild(el('h2', '', title));
            appendChildren(box, node, ctx);
            return box;
        },

        form: function (node, ctx) {
            const props = node.props || {};
            const box = el('fieldset', 'subblock');
            const legend = textOf(props, 'legend_key', 'legend');
            if (legend) box.appendChild(el('legend', '', legend));
            appendChildren(box, node, ctx);

            if (props.submit) {
                box.addEventListener('keydown', function (event) {
                    if (event.key === 'Enter' && event.target.tagName !== 'TEXTAREA') {
                        event.preventDefault();
                        runAction(ctx, props.submit);
                    }
                });
            }
            return box;
        },

        row: function (node, ctx) {
            const props = node.props || {};
            const box = el('div', 'ui-row ui-gap-md');
            if (props.gap) box.classList.add('ui-gap-' + oneOf(props.gap, ['none', 'sm', 'md', 'lg'], 'md'));
            if (props.align) box.classList.add('ui-align-' + oneOf(props.align, ['start', 'center', 'end', 'stretch'], 'center'));
            if (props.wrap === false) box.style.flexWrap = 'nowrap';
            appendChildren(box, node, ctx);
            return box;
        },

        column: function (node, ctx) {
            const props = node.props || {};
            const box = el('div', 'ui-column ui-gap-md');
            if (props.gap) box.classList.add('ui-gap-' + oneOf(props.gap, ['none', 'sm', 'md', 'lg'], 'md'));
            if (props.align) box.classList.add('ui-align-' + oneOf(props.align, ['start', 'center', 'end', 'stretch'], 'start'));
            appendChildren(box, node, ctx);
            return box;
        },

        divider: function () {
            return el('hr', 'ui-divider');
        },

        spacer: function (node) {
            const props = node.props || {};
            return el('div', 'ui-spacer ui-spacer-' + oneOf(props.size, ['sm', 'md', 'lg'], 'md'));
        },

        modal: function (node, ctx) {
            if (node.id) ctx.modals[node.id] = node;
            return null;
        },

        text: function (node) {
            const props = node.props || {};
            const value = props.text_key ? tr(props.text_key, props.value) :
                (props.value == null ? '' : String(props.value));
            const format = props.format || 'plain';

            if (format === 'url') {
                const row = el('div', 'ui-url-row');
                row.appendChild(el('code', 'ui-code', absolutize(value)));
                if (props.copyable !== false) {
                    const button = el('button', 'btn btn-secondary btn-sm', tr('copy', 'Copy'));
                    button.type = 'button';
                    button.addEventListener('click', function () { copyText(absolutize(value), button); });
                    row.appendChild(button);
                }
                return row;
            }

            if (format === 'code') {
                const code = el('code', 'ui-code', value);
                if (!props.copyable) return code;

                const row = el('div', 'ui-url-row');
                row.appendChild(code);
                const button = el('button', 'btn btn-secondary btn-sm', tr('copy', 'Copy'));
                button.type = 'button';
                button.addEventListener('click', function () { copyText(value, button); });
                row.appendChild(button);
                return row;
            }

            const box = el('p', 'ui-text', value);
            if (props.style) box.classList.add('ui-text-' + oneOf(props.style, ['muted', 'strong', 'success', 'danger'], 'default'));
            return box;
        },

        link: function (node) {
            const props = node.props || {};
            const link = el('a', 'btn btn-secondary btn-sm', textOf(props, 'label_key', 'label'));
            link.href = safeHref(props.href) || '#';
            if (props.external) {
                link.target = '_blank';
                link.rel = 'noopener noreferrer';
            }
            return link;
        },

        notice: function (node) {
            const props = node.props || {};
            const style = oneOf(props.style, ['info', 'success', 'warning', 'danger'], 'info');
            const box = el('div', 'ui-notice ui-notice-' + style);
            const title = textOf(props, 'title_key', 'title');
            if (title) box.appendChild(el('strong', '', title));
            box.appendChild(el('span', '', props.text_key ? tr(props.text_key, props.value) :
                (props.value == null ? '' : String(props.value))));
            return box;
        },

        badge: function (node) {
            const props = node.props || {};
            const style = oneOf(props.style, ['info', 'success', 'warning', 'danger'], 'default');
            return el('span', 'ui-badge ui-badge-' + style, badgeText(props));
        },

        progress: function (node) {
            const props = node.props || {};
            const max = Number(props.max > 0 ? props.max : 100);
            const value = Number(props.value) || 0;
            const percent = Math.max(0, Math.min(100, Math.round(value / max * 100)));

            const box = el('div', 'ui-progress');
            const label = el('div', 'ui-progress-label');
            label.appendChild(el('span', '', textOf(props, 'label_key', 'label')));
            if (props.show_percent !== false) label.appendChild(el('span', '', percent + '%'));
            box.appendChild(label);

            const track = el('div', 'ui-progress-track');
            const bar = el('div', 'ui-progress-bar');
            bar.style.width = percent + '%';
            track.appendChild(bar);
            box.appendChild(track);
            return box;
        },

        media: function (node) {
            const props = node.props || {};
            const img = el('img', 'ui-media');
            if (props.ratio) img.classList.add('ui-media-' + oneOf(props.ratio, ['square', 'video', 'poster'], 'auto'));
            img.src = safeSrc(props.src);
            img.alt = textOf(props, 'alt_key', 'alt');
            img.loading = 'lazy';
            return img;
        },

        cards: function (node) {
            const props = node.props || {};
            const box = el('div', 'cards');
            (props.items || []).forEach(function (item) {
                const card = el('div', 'stat-card');
                card.appendChild(el('div', 'label', textOf(item, 'label_key', 'label')));
                const value = el('div', 'value', item.value == null ? '' : String(item.value));
                if (item.style) value.classList.add(oneOf(item.style, ['success', 'warning', 'danger'], 'default'));
                card.appendChild(value);
                box.appendChild(card);
            });
            return box;
        },

        table: function (node, ctx) {
            const props = node.props || {};
            const columns = props.columns || [];
            const rowActions = props.row_actions || [];

            const wrap = el('div', 'table-wrap');
            const table = el('table', 'table');

            const head = el('thead');
            const headRow = el('tr');
            columns.forEach(function (column) {
                const th = el('th', '', textOf(column, 'title_key', 'title'));
                if (column.align) th.style.textAlign = oneOf(column.align, ['center', 'end'], 'start');
                headRow.appendChild(th);
            });
            if (rowActions.length) headRow.appendChild(el('th', '', ''));
            head.appendChild(headRow);
            table.appendChild(head);

            const body = el('tbody');
            const rows = props.rows || [];
            if (!rows.length) {
                const emptyRow = el('tr');
                const cell = el('td', 'hint', textOf(props, 'empty_key', 'empty') || tr('empty', 'Empty'));
                cell.colSpan = Math.max(1, columns.length + (rowActions.length ? 1 : 0));
                emptyRow.appendChild(cell);
                body.appendChild(emptyRow);
            } else {
                rows.forEach(function (row) {
                    const tr = el('tr');
                    if (row.id) tr.setAttribute('data-row', row.id);
                    columns.forEach(function (column) {
                        const raw = row.cells ? row.cells[column.id] : row[column.id];
                        const cell = el('td');
                        if (raw && typeof raw === 'object') {
                            cell.textContent = raw.text_key ? tr(raw.text_key, raw.value) :
                                (raw.value == null ? '' : String(raw.value));
                        } else {
                            cell.textContent = raw == null ? '' : String(raw);
                        }
                        if (column.align) cell.style.textAlign = oneOf(column.align, ['center', 'end'], 'start');
                        tr.appendChild(cell);
                    });

                    if (rowActions.length) {
                        const cell = el('td', 'actions-cell');
                        const box = el('div', 'ui-list-actions');
                        rowActions.forEach(function (action) {
                            const button = el('button', ('btn btn-sm ' + buttonClass(action.style)).trim(),
                                textOf(action, 'label_key', 'label'));
                            button.type = 'button';
                            button.addEventListener('click', function () {
                                runAction(ctx, action.action, { button: button, item: row, params: action.params });
                            });
                            box.appendChild(button);
                        });
                        cell.appendChild(box);
                        tr.appendChild(cell);
                    }

                    body.appendChild(tr);
                    if (global.siloUI && global.siloUI.labels) global.siloUI.labels(tr);
                });
            }

            table.appendChild(body);
            wrap.appendChild(table);
            return wrap;
        },

        list: function (node, ctx) {
            const props = node.props || {};
            const box = el('div', 'ui-list');
            const items = props.items || [];

            if (!items.length) {
                box.appendChild(el('p', 'hint', textOf(props, 'empty_key', 'empty') || tr('empty', 'Empty')));
                return box;
            }

            items.forEach(function (item) {
                const row = el('div', 'ui-list-item');
                if (item.id) row.setAttribute('data-row', item.id);

                const main = el('div', 'ui-list-main');
                main.appendChild(el('div', 'ui-list-title', textOf(item, 'title_key', 'title')));
                const subtitle = textOf(item, 'subtitle_key', 'subtitle');
                if (subtitle) main.appendChild(el('div', 'ui-list-sub', subtitle));
                row.appendChild(main);

                if (item.badge) {
                    const style = oneOf(item.badge.style, ['info', 'success', 'warning', 'danger'], 'default');
                    row.appendChild(el('span', 'ui-badge ui-badge-' + style, badgeText(item.badge)));
                }

                const actions = item.actions || [];
                if (actions.length) {
                    const box2 = el('div', 'ui-list-actions');
                    actions.forEach(function (action) {
                        const button = el('button', ('btn btn-sm ' + buttonClass(action.style)).trim(),
                            textOf(action, 'label_key', 'label'));
                        button.type = 'button';
                        button.addEventListener('click', function () {
                            runAction(ctx, action.action, { button: button, item: item, params: action.params });
                        });
                        box2.appendChild(button);
                    });
                    row.appendChild(box2);
                }

                box.appendChild(row);
            });
            return box;
        },

        grid: function (node, ctx) {
            const props = node.props || {};
            const box = el('div', 'ui-grid');
            if (props.columns) box.classList.add('ui-grid-' + oneOf(props.columns, ['2', '3', '4', '6'], 'auto'));

            const items = props.items || [];
            if (!items.length) {
                box.appendChild(el('p', 'hint', textOf(props, 'empty_key', 'empty') || tr('empty', 'Empty')));
                return box;
            }
            items.forEach(function (item) {
                const rendered = renderNode(item, ctx);
                if (rendered) box.appendChild(rendered);
            });
            return box;
        },

        card: function (node, ctx) {
            const props = node.props || {};
            const card = el('article', 'ui-card');
            if (node.id) card.setAttribute('data-row', node.id);

            const poster = el('div', 'ui-card-poster');
            if (props.poster) {
                const img = el('img');
                img.src = safeSrc(props.poster);
                img.alt = '';
                img.loading = 'lazy';
                poster.appendChild(img);
            } else {
                poster.appendChild(el('div', 'ui-card-placeholder', tr('no_poster', 'No poster')));
            }

            if (props.badge) {
                const style = oneOf(props.badge.style, ['info', 'success', 'warning', 'danger'], 'default');
                poster.appendChild(el('span', 'ui-badge ui-badge-' + style + ' ui-card-badge', badgeText(props.badge)));
            }

            const menu = props.menu || [];
            if (menu.length) {
                const menuBox = el('div', 'ui-card-menu');
                menu.forEach(function (item) {
                    const label = textOf(item, 'label_key', 'label');
                    const button = el('button', '', item.icon ? '' : (label || 'x'));
                    button.type = 'button';
                    button.title = label;
                    if (item.icon) {
                        const img = el('img');
                        img.src = safeSrc(item.icon);
                        img.alt = '';
                        img.width = 14;
                        img.height = 14;
                        button.appendChild(img);
                    }
                    button.addEventListener('click', function (event) {
                        event.stopPropagation();
                        runAction(ctx, item.action, { button: button, item: props, params: item.params });
                    });
                    menuBox.appendChild(button);
                });
                poster.appendChild(menuBox);
            }
            card.appendChild(poster);

            card.appendChild(el('div', 'ui-card-title', textOf(props, 'title_key', 'title')));
            const subtitle = textOf(props, 'subtitle_key', 'subtitle');
            if (subtitle) card.appendChild(el('div', 'ui-card-sub', subtitle));

            const meta = props.meta || [];
            if (meta.length) {
                const metaBox = el('div', 'ui-card-meta');
                meta.forEach(function (item) {
                    const span = el('span', '', item.text_key ? tr(item.text_key, item.value) :
                        (item.value == null ? '' : String(item.value)));
                    metaBox.appendChild(span);
                });
                card.appendChild(metaBox);
            }

            if (props.action) {
                card.style.cursor = 'pointer';
                card.addEventListener('click', function () {
                    runAction(ctx, props.action, { item: props });
                });
            }
            return card;
        },

        player: function (node) {
            const props = node.props || {};
            const media = document.createElement(props.type === 'audio' ? 'audio' : 'video');
            media.className = 'ui-player';
            media.controls = true;
            media.preload = 'metadata';
            if (props.autoplay) media.autoplay = true;
            if (props.poster) media.poster = safeSrc(props.poster);
            if (props.src) {
                const source = document.createElement('source');
                source.src = safeSrc(props.src);
                media.appendChild(source);
            }
            return media;
        },

        field: function (node, ctx) {
            const props = node.props || {};
            const id = node.id;
            const input = props.input || 'text';
            const wrap = el('div', 'ui-field');

            if (input === 'hidden') {
                const hidden = document.createElement('input');
                hidden.type = 'hidden';
                hidden.value = props.value == null ? '' : String(props.value);
                ctx.fields[id] = hidden;
                ctx.values[id] = readField(hidden);
                wrap.appendChild(hidden);
                return wrap;
            }

            const label = textOf(props, 'label_key', 'label');
            if (input === 'checkbox' || input === 'switch') {
                const box = el('label', 'ui-check');
                const check = document.createElement('input');
                check.type = 'checkbox';
                check.id = 'ui-f-' + id;
                check.checked = !!props.value;
                check.disabled = !!props.disabled;
                if (input === 'switch') check.setAttribute('role', 'switch');
                check.addEventListener('change', function () { ctx.values[id] = readField(check); });
                box.appendChild(check);
                box.appendChild(el('span', '', label));
                wrap.appendChild(box);
                ctx.fields[id] = check;
                ctx.values[id] = readField(check);
            } else {
                const box = el('label', 'field');
                box.appendChild(el('span', '', label));

                let control;
                if (input === 'textarea') {
                    control = el('textarea', 'input' + (props.monospace ? ' ui-textarea' : ''));
                    control.rows = props.rows || 4;
                    control.value = props.value == null ? '' : String(props.value);
                } else if (input === 'select') {
                    control = el('select', 'input');
                    (props.options || []).forEach(function (option) {
                        const opt = el('option', '', option.label_key ? tr(option.label_key, option.label) :
                            (option.label == null ? String(option.value) : option.label));
                        opt.value = option.value == null ? '' : String(option.value);
                        if (String(option.value) === String(props.value)) opt.selected = true;
                        control.appendChild(opt);
                    });
                } else {
                    control = document.createElement('input');
                    control.className = 'input';
                    control.type = input;
                    if (props.value != null) control.value = String(props.value);
                    if (props.min != null) control.min = props.min;
                    if (props.max != null) control.max = props.max;
                    if (props.step != null) control.step = props.step;
                }

                control.id = 'ui-f-' + id;
                control.disabled = !!props.disabled;
                if (props.readonly) control.readOnly = true;
                if (props.required) control.required = true;
                if (props.placeholder_key || props.placeholder) {
                    control.placeholder = textOf(props, 'placeholder_key', 'placeholder');
                }
                if (props.autocomplete) control.autocomplete = props.autocomplete;
                control.addEventListener('input', function () { ctx.values[id] = readField(control); });
                control.addEventListener('change', function () { ctx.values[id] = readField(control); });

                box.appendChild(control);
                wrap.appendChild(box);
                ctx.fields[id] = control;
                ctx.values[id] = readField(control);
            }

            const hint = textOf(props, 'hint_key', 'hint');
            if (hint) wrap.appendChild(el('p', 'hint', hint));

            const error = el('p', 'hint ui-error', '');
            error.id = 'ui-err-' + id;
            error.hidden = true;
            wrap.appendChild(error);
            return wrap;
        },

        button: function (node, ctx) {
            const props = node.props || {};
            const button = el('button', ('btn ' + buttonClass(props.style)).trim(),
                textOf(props, 'label_key', 'label'));
            button.type = 'button';
            button.disabled = !!props.disabled;
            if (props.icon) {
                const img = el('img');
                img.src = safeSrc(props.icon);
                img.alt = '';
                img.width = 14;
                img.height = 14;
                button.prepend(img);
            }
            if (props.action) {
                button.addEventListener('click', function () {
                    runAction(ctx, props.action, { button: button });
                });
            }
            return button;
        },

        button_row: function (node, ctx) {
            const props = node.props || {};
            const box = el('div', 'row actions-row ui-gap-sm');
            if (props.align) box.classList.add('ui-align-' + oneOf(props.align, ['start', 'center', 'end'], 'end'));
            appendChildren(box, node, ctx);
            return box;
        },
    };

    // createContext готовит состояние рендера.
    function createContext(plugin, page, root) {
        return {
            plugin: plugin,
            page: page,
            root: root,
            base: '/plugins/' + encodeURIComponent(plugin) + '/',
            doc: null,
            fields: {},
            values: {},
            modals: {},
            timers: [],
            modalKey: null,
        };
    }

    // start запускает страницу схемы из адреса.
    function start() {
        const root = document.getElementById('ui-root');
        if (!root) return;

        const match = location.pathname.match(/^\/plugins\/([^/]+)(?:\/ui(?:\/([^/]+))?)?\/?$/);
        if (!match) {
            showFatal({ root: root }, 'Not a plugin page');
            return;
        }

        const plugin = decodeURIComponent(match[1]);
        const page = decodeURIComponent(match[2] || '') ||
            new URLSearchParams(location.search).get('page') || 'main';
        const ctx = createContext(plugin, page, root);
        load(ctx);
    }

    // render рисует готовый документ в контейнер.
    function render(doc, container, opts) {
        opts = opts || {};
        const ctx = createContext(opts.plugin || '', opts.page || 'main', container);
        mount(ctx, doc);
        return ctx;
    }

    global.SiloUI = {
        start: start,
        render: render,
        registry: registry,
    };
})(window);
