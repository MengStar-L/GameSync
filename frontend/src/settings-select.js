let selectSequence = 0;
let closeActiveSelect = null;

export function moveSelectIndex(key, current, count) {
  if (count <= 0) return -1;
  if (key === "ArrowDown") return (current + 1 + count) % count;
  if (key === "ArrowUp") return (current - 1 + count) % count;
  if (key === "Home") return 0;
  if (key === "End") return count - 1;
  return current;
}

export function shouldOpenSelectUp(triggerRect, menuHeight, viewportHeight, gap = 5) {
  const spaceBelow = viewportHeight - triggerRect.bottom - gap;
  const spaceAbove = triggerRect.top - gap;
  return menuHeight > spaceBelow && spaceAbove > spaceBelow;
}

export function createSelectField({ h, icon, label, options, current, help, documentRef = document }) {
  const id = `set-select-${++selectSequence}`;
  const normalized = options.map(([value, text]) => ({ value: String(value), text }));
  let value = normalized.some((option) => option.value === String(current))
    ? String(current)
    : normalized[0]?.value || "";
  let activeIndex = Math.max(0, normalized.findIndex((option) => option.value === value));
  let opened = false;

  const valueEl = h("span", { class: "set-select-value" });
  const menu = h("div", {
    class: "set-select-menu",
    id: `${id}-listbox`,
    role: "listbox",
    hidden: true,
  });
  const trigger = h(
    "button",
    {
      class: "set-select-trigger",
      type: "button",
      role: "combobox",
      "aria-label": label,
      "aria-haspopup": "listbox",
      "aria-expanded": "false",
      "aria-controls": `${id}-listbox`,
    },
    valueEl,
    h("span", { class: "set-select-chevron", html: icon("chevronDown") }),
  );
  const root = h("div", { class: "set-select" }, trigger, menu);
  const optionEls = normalized.map((option, index) => {
    const optionEl = h(
      "button",
      {
        class: "set-select-option",
        id: `${id}-option-${index}`,
        type: "button",
        role: "option",
        tabindex: "-1",
        onClick: () => commit(index),
      },
      h("span", { class: "set-select-option-label" }, option.text),
      h("span", { class: "set-select-check", html: icon("check") }),
    );
    menu.append(optionEl);
    return optionEl;
  });

  function syncVisualState() {
    const selectedIndex = normalized.findIndex((option) => option.value === value);
    valueEl.textContent = normalized[selectedIndex]?.text || "";
    optionEls.forEach((optionEl, index) => {
      optionEl.classList.toggle("active", opened && index === activeIndex);
      optionEl.setAttribute("aria-selected", String(index === selectedIndex));
    });
    if (opened && optionEls[activeIndex]) {
      trigger.setAttribute("aria-activedescendant", optionEls[activeIndex].id);
      optionEls[activeIndex].scrollIntoView({ block: "nearest" });
    } else {
      trigger.removeAttribute("aria-activedescendant");
    }
  }

  function close({ focus = false } = {}) {
    if (!opened) return;
    opened = false;
    menu.hidden = true;
    root.classList.remove("open-up");
    trigger.setAttribute("aria-expanded", "false");
    if (closeActiveSelect === close) closeActiveSelect = null;
    syncVisualState();
    if (focus) trigger.focus();
  }

  function open() {
    closeActiveSelect?.();
    opened = true;
    closeActiveSelect = close;
    activeIndex = Math.max(0, normalized.findIndex((option) => option.value === value));
    menu.hidden = false;
    const viewportHeight = documentRef.defaultView?.innerHeight ?? Number.POSITIVE_INFINITY;
    root.classList.toggle(
      "open-up",
      shouldOpenSelectUp(trigger.getBoundingClientRect(), menu.getBoundingClientRect().height, viewportHeight),
    );
    trigger.setAttribute("aria-expanded", "true");
    syncVisualState();
  }

  function commit(index) {
    if (!normalized[index]) return;
    value = normalized[index].value;
    activeIndex = index;
    close({ focus: true });
    syncVisualState();
  }

  function onKeyDown(event) {
    if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      event.preventDefault();
      if (!opened) open();
      activeIndex = moveSelectIndex(event.key, activeIndex, normalized.length);
      syncVisualState();
      return;
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      if (opened) commit(activeIndex);
      else open();
      return;
    }
    if (event.key === "Escape" && opened) {
      event.preventDefault();
      close({ focus: true });
    }
  }

  function onTriggerClick() {
    if (opened) close();
    else open();
  }

  function onDocumentPointerDown(event) {
    if (opened && !root.contains(event.target)) close();
  }

  trigger.addEventListener("click", onTriggerClick);
  trigger.addEventListener("keydown", onKeyDown);
  documentRef.addEventListener("pointerdown", onDocumentPointerDown);
  syncVisualState();

  const field = h(
    "div",
    { class: "field" },
    h("span", { class: "field-label" }, label),
    root,
    help ? h("span", { class: "field-help" }, help) : null,
  );

  return {
    field,
    get value() {
      return value;
    },
    destroy() {
      close();
      trigger.removeEventListener("click", onTriggerClick);
      trigger.removeEventListener("keydown", onKeyDown);
      documentRef.removeEventListener("pointerdown", onDocumentPointerDown);
    },
  };
}
