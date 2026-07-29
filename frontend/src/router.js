// ============================================================
// router.js —— 内部路由：navigate(page, params) / back() / onChange
// 视图 mount(root, ctx) 返回 cleanup；切页时先 cleanup 再清空。
// ============================================================

const registry = new Map();
const listeners = new Set();
const stack = [];

let current = { page: "", params: {} };
let cleanup = null;
let rootEl = null;
let ctx = null;

export const router = {
  register(map, context, root) {
    for (const [name, mountFn] of Object.entries(map)) registry.set(name, mountFn);
    ctx = context;
    rootEl = root;
  },

  get current() {
    return current;
  },

  onChange(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
  },

  navigate(page, params = {}, { push = true } = {}) {
    const mountFn = registry.get(page);
    if (!mountFn) {
      console.warn(`router: 未注册的页面 ${page}`);
      return;
    }
    if (push && current.page) stack.push(current);
    if (stack.length > 30) stack.shift();

    try {
      cleanup?.();
    } catch (e) {
      console.error(e);
    }
    cleanup = null;
    rootEl.innerHTML = "";
    rootEl.scrollTop = 0;

    current = { page, params };
    cleanup = mountFn(rootEl, { ...ctx, params }) || null;
    listeners.forEach((fn) => {
      try {
        fn(current);
      } catch (e) {
        console.error(e);
      }
    });
  },

  back(fallback = "library") {
    const prev = stack.pop();
    if (prev) router.navigate(prev.page, prev.params, { push: false });
    else router.navigate(fallback, {}, { push: false });
  },
};
