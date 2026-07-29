// ============================================================
// views/accounts.js —— 云账号票券墙（前缀 .acc-）
// 票券卡（撕票齿孔分隔线）+ 右侧抽屉编辑表单。
// 第一个账号为主账号（D1 索引中心），其余为存储副账号。
// ============================================================

const QUOTA_BYTES = 10 * 2 ** 30; // R2 免费额度 10GB

export function mount(root, ctx) {
  const { store, ui, api } = ctx;
  const { h, icon, iconEl, fmtTime, fmtBytes, toast } = ui;

  const verifying = new Set(); // 验证中的账号 id
  const toggling = new Set(); // 开关保存中的账号 id
  let activeDrawer = null;
  let disposed = false;

  /* ---------------- 工具 ---------------- */

  const errMsg = (e) => e?.message || String(e || "未知错误");

  function maskId(value) {
    const s = String(value || "");
    if (!s) return "未填写";
    if (s.length <= 12) return s;
    return `${s.slice(0, 8)}…${s.slice(-4)}`;
  }

  // provider 为空一律按 cloudflare 处理（历史数据兼容）
  const isWebdav = (acc) => (acc?.provider || "cloudflare") === "webdav";

  const providerLabel = (provider) => (provider === "webdav" ? "WebDAV" : "Cloudflare R2");

  function webdavHost(url) {
    const s = String(url || "");
    if (!s) return "未填写";
    try {
      return new URL(s).host || s;
    } catch {
      return s;
    }
  }

  const isValid = (acc) => acc.verificationState === "valid" && !acc.lastError;
  const isInvalid = (acc) => acc.verificationState === "invalid" || Boolean(acc.lastError);

  function verifyBadge(acc) {
    if (verifying.has(acc.id)) {
      return h("span", { class: "badge info acc-badge-busy" }, iconEl("refresh"), "验证中…");
    }
    if (isValid(acc)) return h("span", { class: "badge ok" }, iconEl("check"), "已验证");
    if (isInvalid(acc)) return h("span", { class: "badge err" }, iconEl("alert"), "验证失败");
    return h("span", { class: "badge mute" }, iconEl("clock"), "未验证");
  }

  /* ---------------- 动作 ---------------- */

  async function onVerify(acc) {
    if (verifying.has(acc.id)) return;
    verifying.add(acc.id);
    render();
    try {
      await store.actions.verifyAccount(acc.id);
      const next = store.select.accounts().find((a) => a.id === acc.id);
      if (next) {
        if (isValid(next)) toast(`「${next.name}」验证通过`, "ok");
        else toast(next.lastError || `「${next.name}」验证失败`, "err");
      }
    } catch (e) {
      toast(`验证失败：${errMsg(e)}`, "err");
    } finally {
      verifying.delete(acc.id);
      if (!disposed) render();
    }
  }

  async function onToggle(acc, enabled, input) {
    if (toggling.has(acc.id)) {
      input.checked = Boolean(acc.enabled);
      return;
    }
    toggling.add(acc.id);
    input.disabled = true;
    try {
      // 脏字段提交：以保存时 store 里的最新账号为底、仅覆盖 enabled，
      // 避免渲染时快照回滚他机对其它字段的改动
      const latest = store.select.accounts().find((a) => a.id === acc.id);
      if (!latest) {
        toast("该账号已在其他设备被删除", "err");
        return;
      }
      await store.actions.saveAccount({ ...latest, enabled });
      toast(enabled ? `「${acc.name}」已启用` : `「${acc.name}」已停用`, enabled ? "ok" : "info");
    } catch (e) {
      toast(`保存失败：${errMsg(e)}`, "err");
    } finally {
      toggling.delete(acc.id);
      if (!disposed) render();
    }
  }

  /* ---------------- 表单构件（抽屉表单与切换对话框共用） ---------------- */

  // 输入控件工厂：refs 收集输入引用、dirty 记录用户实际改过的字段
  // （新建/切换表单整包提交，dirty 仅占位不参与判断）
  function formControls(refs, dirty) {
    const textInput = (key, value, placeholder, mono) => {
      const input = h("input", {
        class: `input${mono ? " acc-mono" : ""}`,
        type: "text",
        value: value || "",
        placeholder: placeholder || "",
        spellcheck: "false",
        autocomplete: "off",
        onInput: () => dirty.add(key),
      });
      refs[key] = input;
      return input;
    };

    const secretInput = (key, value, placeholder) => {
      const input = h("input", {
        class: "input",
        type: "password",
        value: value || "",
        placeholder: placeholder || "",
        spellcheck: "false",
        autocomplete: "new-password",
        onInput: () => dirty.add(key),
      });
      refs[key] = input;
      let shown = false;
      const eye = h("button", {
        type: "button",
        class: "acc-eye",
        "aria-label": "显示或隐藏",
        html: icon("eye"),
        onClick: () => {
          shown = !shown;
          input.type = shown ? "text" : "password";
          eye.innerHTML = icon(shown ? "eyeOff" : "eye");
        },
      });
      return h("div", { class: "acc-secret" }, input, eye);
    };

    const field = (label, required, control, help) =>
      h(
        "div",
        { class: "field" },
        h("label", { class: "field-label" }, label, required ? h("span", { class: "acc-req" }, " *") : null),
        control,
        help ? h("div", { class: "field-help" }, help) : null,
      );

    return { textInput, secretInput, field };
  }

  // Cloudflare 字段组（primaryForm 决定是否含 API Token 与 D1 Database ID）
  function buildCfFields({ field, textInput, secretInput }, acc, primaryForm) {
    const group = h("div", { class: "acc-fgroup" });
    group.append(
      field(
        "Account ID",
        true,
        textInput("accountId", acc?.accountId, "Cloudflare Account ID", true),
        "Cloudflare 仪表盘右侧的 32 位账户标识",
      ),
    );
    if (primaryForm) {
      group.append(
        field(
          "API Token",
          true,
          secretInput("apiToken", acc?.apiToken, "具备 D1 与 R2 编辑权限的 API Token"),
          "主账号用它维护 D1 目录索引",
        ),
      );
    }
    group.append(field("R2 Bucket", true, textInput("r2Bucket", acc?.r2Bucket, "例如 gamesync-saves", true)));
    if (primaryForm) {
      group.append(
        field(
          "D1 Database ID",
          true,
          textInput("d1DatabaseId", acc?.d1DatabaseId, "D1 数据库 UUID", true),
          "承载目录索引的 D1 数据库",
        ),
      );
    }
    group.append(
      field("R2 Access Key ID", true, textInput("r2AccessKeyId", acc?.r2AccessKeyId, "R2 API 令牌的 Access Key ID", true)),
      field("R2 Secret Access Key", true, secretInput("r2SecretAccessKey", acc?.r2SecretAccessKey, "R2 API 令牌的 Secret Access Key")),
    );
    return group;
  }

  // WebDAV 字段组（服务器地址/用户名/密码必填，根目录选填）
  function buildWdFields({ field, textInput, secretInput }, acc) {
    const group = h("div", { class: "acc-fgroup" });
    group.append(
      field(
        "服务器地址",
        true,
        textInput("webdavUrl", acc?.webdavUrl, "https://dav.example.com/remote.php/dav/files/user", true),
        "WebDAV 服务器完整地址，Nextcloud / 坚果云 / NAS 均可",
      ),
      field("用户名", true, textInput("webdavUsername", acc?.webdavUsername, "WebDAV 登录用户名")),
      field("密码", true, secretInput("webdavPassword", acc?.webdavPassword, "建议使用应用专用密码")),
      field(
        "根目录",
        false,
        textInput("webdavRoot", acc?.webdavRoot, "GameSync", true),
        "存放同步数据的服务器目录，留空使用 GameSync",
      ),
    );
    return group;
  }

  // 必填校验清单（抽屉与切换对话框同一套）
  const requiredFieldsFor = (provider, primaryForm) =>
    provider === "webdav"
      ? [
          ["webdavUrl", "服务器地址"],
          ["webdavUsername", "用户名"],
          ["webdavPassword", "密码"],
        ]
      : [
          ["accountId", "Account ID"],
          ...(primaryForm ? [["apiToken", "API Token"]] : []),
          ["r2Bucket", "R2 Bucket"],
          ...(primaryForm ? [["d1DatabaseId", "D1 Database ID"]] : []),
          ["r2AccessKeyId", "R2 Access Key ID"],
          ["r2SecretAccessKey", "R2 Secret Access Key"],
        ];

  /* ---------------- 抽屉表单（新建/编辑共用） ---------------- */

  function openForm(acc, presetProvider) {
    if (activeDrawer) return;
    const isNew = !acc;
    // 主账号表单：编辑看 isPrimary；新建看当前是否还没有任何账号
    const primaryForm = acc ? Boolean(acc.isPrimary) : store.select.accounts().length === 0;
    // 存储方式：编辑固定为账号自身；新建可用分段切换（welcome 传参预选）
    let provider = acc
      ? isWebdav(acc)
        ? "webdav"
        : "cloudflare"
      : presetProvider === "webdav"
        ? "webdav"
        : "cloudflare";

    activeDrawer = ui.drawer({
      title: isNew ? "添加账号" : `编辑「${acc.name}」`,
      width: 560,
      onClose: () => {
        activeDrawer = null;
      },
      render(body, close) {
        const refs = {};
        // 脏字段集合：只提交用户实际改过的字段，保存时与 store 最新账号合并，
        // 抽屉长开期间他机的改动不会被打开时刻的快照回滚
        const dirty = new Set();
        const controls = formControls(refs, dirty);

        // 说明横幅与角色小字随 provider 切换，文本节点复用
        const bannerFor = () =>
          provider === "webdav"
            ? "WebDAV 主同步空间会把目录索引与存档对象都存进服务器上的指定目录，建议使用应用专用密码。"
            : primaryForm
              ? "主账号除 R2 凭据外，还需要 API Token 与 D1 Database ID，用于承载游戏目录索引。"
              : "副账号只需填写 R2 凭据，作为纯存储节点接入。";
        const roleSubFor = () =>
          provider === "webdav"
            ? "主同步空间 · WebDAV"
            : primaryForm
              ? "主账号 · D1 索引中心"
              : "副账号 · 存储节点";
        const bannerText = h("span", {}, bannerFor());
        const roleSub = h("div", { class: "acc-form-role-sub" }, roleSubFor());

        // 新建时的 provider 分段选择；编辑时只读展示徽章
        let segButtons = null;
        if (isNew) {
          const segItem = (value, label) =>
            h(
              "button",
              { type: "button", class: `seg-item${provider === value ? " active" : ""}`, onClick: () => setProvider(value) },
              label,
            );
          segButtons = { cloudflare: segItem("cloudflare", "Cloudflare"), webdav: segItem("webdav", "WebDAV") };
          body.append(
            h(
              "div",
              { class: "field acc-provider-field" },
              h("span", { class: "field-label" }, "存储方式"),
              h("div", { class: "seg acc-provider-seg" }, segButtons.cloudflare, segButtons.webdav),
            ),
          );
        }

        // 说明横幅 + 角色只读提示（账号名与主副角色由后端自动分配）
        body.append(
          h("div", { class: "acc-banner info acc-banner-form" }, iconEl("info"), bannerText),
          h(
            "div",
            { class: "acc-form-role" },
            iconEl(primaryForm ? "database" : "hardDrive"),
            h(
              "div",
              {},
              h("div", { class: "acc-form-role-name" }, acc ? acc.name : "账号名将由系统自动分配"),
              roleSub,
            ),
            isNew
              ? null
              : h("span", { class: `badge ${provider === "webdav" ? "info" : "mute"}` }, provider === "webdav" ? "WebDAV" : "Cloudflare"),
          ),
        );

        // 两组字段同时构建、按 provider 显隐，切换不丢已填内容
        const cfFields = buildCfFields(controls, acc, primaryForm);
        const wdFields = buildWdFields(controls, acc);

        function syncProviderUI() {
          segButtons?.cloudflare.classList.toggle("active", provider === "cloudflare");
          segButtons?.webdav.classList.toggle("active", provider === "webdav");
          cfFields.classList.toggle("acc-fgroup-off", provider !== "cloudflare");
          wdFields.classList.toggle("acc-fgroup-off", provider !== "webdav");
          bannerText.textContent = bannerFor();
          roleSub.textContent = roleSubFor();
        }

        function setProvider(next) {
          if (provider === next) return;
          provider = next;
          syncProviderUI();
        }

        const form = h("div", { class: "acc-form" });
        form.append(cfFields, wdFields);
        syncProviderUI();

        const enabledInput = h("input", {
          type: "checkbox",
          checked: isNew ? true : Boolean(acc.enabled),
          onChange: () => dirty.add("enabled"),
        });
        form.append(h("label", { class: "check acc-form-check" }, enabledInput, h("span", {}, "启用该账号")));

        const saveBtn = h("button", { class: "btn btn-primary", html: `${icon("check")}<span>保存</span>` });
        saveBtn.addEventListener("click", async () => {
          if (saveBtn.classList.contains("busy")) return;
          const latest = isNew ? null : store.select.accounts().find((a) => a.id === acc.id);
          if (!isNew && !latest) {
            toast("该账号已在其他设备被删除", "err");
            close();
            return;
          }
          if (!isNew && !dirty.size) {
            toast("没有修改", "info");
            return;
          }
          // 新建：全部字段提交；编辑：最新账号展开 + 仅脏字段覆盖（只提交当前 provider 的字段组）
          const editableKeys =
            provider === "webdav"
              ? ["webdavUrl", "webdavUsername", "webdavPassword", "webdavRoot"]
              : [
                  "accountId",
                  "r2Bucket",
                  "r2AccessKeyId",
                  "r2SecretAccessKey",
                  ...(primaryForm ? ["apiToken", "d1DatabaseId"] : []),
                ];
          const payload = isNew ? { provider, apiToken: "", d1DatabaseId: "" } : { ...latest };
          for (const key of editableKeys) {
            if (isNew || dirty.has(key)) payload[key] = refs[key].value.trim();
          }
          if (isNew || dirty.has("enabled")) payload.enabled = enabledInput.checked;
          // webdav 根目录留空时落默认值，与后端约定一致
          if (provider === "webdav" && !String(payload.webdavRoot || "").trim()) payload.webdavRoot = "GameSync";
          const required = requiredFieldsFor(provider, primaryForm);
          // 校验合并后的提交值：非脏字段以 store 最新值为准，而非输入框快照
          for (const [key, label] of required) {
            if (!String(payload[key] || "").trim()) {
              toast(`请填写 ${label}`, "warn");
              refs[key]?.focus();
              return;
            }
          }
          saveBtn.classList.add("busy");
          saveBtn.innerHTML = `${icon("refresh")}<span>保存中…</span>`;
          try {
            await store.actions.saveAccount(payload);
            toast(isNew ? "账号已添加，建议点击「验证」检查连通性" : "账号已保存", "ok");
            close();
          } catch (e) {
            toast(`保存失败：${errMsg(e)}`, "err");
            saveBtn.classList.remove("busy");
            saveBtn.innerHTML = `${icon("check")}<span>保存</span>`;
          }
        });

        body.append(
          form,
          h(
            "div",
            { class: "acc-form-foot" },
            h("button", { class: "btn btn-ghost", onClick: () => close() }, "取消"),
            saveBtn,
          ),
        );
      },
    });
  }

  /* ---------------- 存储方式切换 ---------------- */

  let switchFlowBusy = false; // 确认/预同步阶段防重入
  let activeSwitchDialog = null; // 凭据或进度对话框（同一时间至多一个）

  const needsRecoveryPassword = (value) => String(value || "").includes("恢复密码");

  function promptRecoveryPassword() {
    return new Promise((resolve) => {
      let settled = false;
      const settle = (value, close) => {
        if (!settled) {
          settled = true;
          resolve(value);
        }
        close?.();
      };
      ui.dialog({
        title: "设置恢复密码",
        width: 420,
        onClose: () => settle(false),
        render(body, close) {
          const passwordInput = h("input", {
            class: "input",
            type: "password",
            autocomplete: "new-password",
            placeholder: "输入恢复密码",
          });
          const errText = h("span");
          const errBanner = h("div", { class: "acc-banner err acc-switch-err" }, iconEl("alert"), errText);
          const saveBtn = h("button", { class: "btn btn-primary" }, iconEl("check"), "保存并继续");
          const save = async () => {
            if (saveBtn.disabled) return;
            const password = passwordInput.value.trim();
            if (!password) {
              errText.textContent = "恢复密码不能为空";
              errBanner.classList.add("show");
              passwordInput.focus();
              return;
            }
            saveBtn.disabled = true;
            errBanner.classList.remove("show");
            try {
              await store.actions.setRecoveryPassword(password);
              toast("恢复密码已设置", "ok");
              settle(true, close);
            } catch (e) {
              errText.textContent = errMsg(e);
              errBanner.classList.add("show");
              saveBtn.disabled = false;
            }
          };
          saveBtn.addEventListener("click", save);
          passwordInput.addEventListener("keydown", (event) => {
            if (event.key === "Enter") save();
          });
          body.append(
            h("label", { class: "field-label" }, "恢复密码"),
            passwordInput,
            errBanner,
            h(
              "div",
              { class: "acc-form-foot" },
              h("button", { class: "btn btn-ghost", onClick: () => settle(false, close) }, "取消"),
              saveBtn,
            ),
          );
          window.setTimeout(() => passwordInput.focus(), 0);
        },
      });
    });
  }

  function startSwitch() {
    if (switchFlowBusy || activeSwitchDialog) return;
    if (store.select.syncingAll()) {
      toast("正在执行全部同步，请稍候再试", "warn");
      return;
    }
    openSwitchDialog();
  }

  async function prepareSwitch(request) {
    if (switchFlowBusy || disposed) return;
    switchFlowBusy = true;
    try {
      const confirmed = await ui.confirm({
        message: "切换前将先同步当前连接。目标连接验证成功后，旧连接会停用但保留，是否继续？",
        confirmText: "同步并切换",
        cancelText: "取消",
      });
      if (!confirmed || disposed) return;

      const summary = await store.actions.syncAll();
      if (summary.busy || disposed) return;
      let useLocalData = false;
      if (summary.incomplete.length) {
        const names = summary.incomplete.map((result) => `「${result.gameName}」`).join("、");
        const useLocal = await ui.confirm({
          message: `以下游戏未完成同步：${names}。是否使用当前本地数据继续切换？`,
          confirmText: "使用本地数据",
          cancelText: "取消切换",
        });
        if (!useLocal || disposed) return;
        useLocalData = true;
      }
      runSwitch({ ...request, useLocalData });
    } finally {
      switchFlowBusy = false;
    }
  }

  // 目标 provider 固定为当前方式的另一种；已有账号优先复用，也可新建连接。
  function openSwitchDialog() {
    if (activeSwitchDialog) return;
    const current = store.select.accounts().find((a) => a.isPrimary);
    const targetProvider = current && isWebdav(current) ? "cloudflare" : "webdav";
    const candidates = store.select
      .accounts()
      .filter(
        (account) =>
          account.id !== current?.id && (isWebdav(account) ? "webdav" : "cloudflare") === targetProvider,
      );
    let selection = candidates.length === 1 ? candidates[0].id : candidates.length ? "" : "new";

    activeSwitchDialog = ui.dialog({
      title: "切换存储方式",
      width: 560,
      onClose: () => {
        activeSwitchDialog = null;
      },
      render(body, close) {
        const refs = {};
        const controls = formControls(refs, new Set());

        const targetSelect = h("select", {
          class: "input acc-switch-select",
          "aria-label": "目标连接",
          onChange: (event) => {
            selection = event.target.value;
            syncSelectionUI();
          },
        });
        if (candidates.length > 1) {
          targetSelect.append(h("option", { value: "", disabled: true }, "请选择连接"));
        }
        for (const account of candidates) {
          const detail = isWebdav(account) ? webdavHost(account.webdavUrl) : maskId(account.accountId);
          targetSelect.append(
            h(
              "option",
              { value: account.id },
              `${account.name} · ${detail}${account.enabled ? "" : "（已停用）"}`,
            ),
          );
        }
        targetSelect.append(h("option", { value: "new" }, `添加新的 ${providerLabel(targetProvider)} 连接`));
        targetSelect.value = selection;

        const bannerText = h("span");
        const newFields =
          targetProvider === "webdav" ? buildWdFields(controls, null) : buildCfFields(controls, null, true);
        const form = h("div", { class: "acc-form acc-switch-new-fields" }, newFields);

        function syncSelectionUI() {
          const adding = selection === "new";
          form.classList.toggle("acc-fgroup-off", !adding);
          if (!selection) {
            bannerText.textContent = "请选择要复用的连接，或添加一个新连接。";
          } else {
            bannerText.textContent = adding
              ? `填写新的 ${providerLabel(targetProvider)} 连接。验证成功后才会保存并切换。`
              : "将复用该账号已保存的连接信息。旧连接会停用但保留，可在以后直接切回。";
          }
        }

        const goBtn = h("button", { class: "btn btn-primary", html: `${icon("refresh")}<span>开始切换</span>` });
        goBtn.addEventListener("click", () => {
          if (!selection) {
            toast("请选择目标连接", "warn");
            targetSelect.focus();
            return;
          }

          let request;
          if (selection === "new") {
            const account = { provider: targetProvider, apiToken: "", d1DatabaseId: "", enabled: true };
            const keys =
              targetProvider === "webdav"
                ? ["webdavUrl", "webdavUsername", "webdavPassword", "webdavRoot"]
                : ["accountId", "apiToken", "r2Bucket", "d1DatabaseId", "r2AccessKeyId", "r2SecretAccessKey"];
            for (const key of keys) account[key] = refs[key].value.trim();
            if (targetProvider === "webdav" && !account.webdavRoot) account.webdavRoot = "GameSync";
            for (const [key, label] of requiredFieldsFor(targetProvider, true)) {
              if (!String(account[key] || "").trim()) {
                toast(`请填写 ${label}`, "warn");
                refs[key]?.focus();
                return;
              }
            }
            request = { newAccount: account };
          } else {
            request = { existingAccountId: selection };
          }
          close();
          prepareSwitch(request);
        });

        syncSelectionUI();

        body.append(
          h(
            "div",
            { class: "acc-switch-target" },
            h(
              "div",
              {},
              h("div", { class: "field-label" }, "新的存储方式"),
              h("div", { class: "acc-switch-provider" }, providerLabel(targetProvider)),
            ),
            h("span", { class: "badge info" }, iconEl(targetProvider === "webdav" ? "cloud" : "database"), "目标"),
          ),
          h("label", { class: "field-label" }, "目标连接"),
          targetSelect,
          h("div", { class: "acc-banner info acc-banner-form" }, iconEl("info"), bannerText),
          form,
          h(
            "div",
            { class: "acc-form-foot" },
            h("button", { class: "btn btn-ghost", onClick: () => close() }, "取消"),
            goBtn,
          ),
        );
      },
    });
  }

  // d+e：进度对话框 + 调后端切换；对话框关闭即退订进度事件（切换在后端继续执行）
  function runSwitch(request) {
    const stageDefs = [
      ["verify", "验证目标连接"],
      ["source_sync", "同步当前连接"],
      ["inventory", "生成迁移清单"],
      ["copy", "校验并复制数据"],
      ["target", "发布目标目录"],
      ["handoff", "提交连接交接"],
      ["commit", "切换本地连接"],
      ["sync", "首次正常同步"],
    ];
    const stageIndex = new Map(stageDefs.map(([key], i) => [key, i]));
    let offEvent = () => {};

    activeSwitchDialog = ui.dialog({
      title: "正在切换存储方式",
      width: 460,
      onClose: () => {
        offEvent();
        activeSwitchDialog = null;
      },
      render(body, close) {
        const rows = stageDefs.map(([key, label]) => {
          const ico = h("span", { class: "acc-switch-ico", html: icon("clock") });
          const count = h("span", { class: "acc-switch-count" });
          const el = h("div", { class: "acc-switch-step" }, ico, h("span", { class: "acc-switch-label" }, label), count);
          return { key, el, ico, count };
        });
        const msgLine = h("div", { class: "acc-switch-msg" }, "正在准备切换…");
        const errText = h("span", {});
        const errBanner = h("div", { class: "acc-banner err acc-switch-err" }, iconEl("alert"), errText);
        const doneBtn = h("button", { class: "btn btn-primary", disabled: true, onClick: () => close() }, "完成");

        const setRowState = (row, stateName) => {
          row.el.classList.remove("active", "done", "fail");
          if (stateName !== "pending") row.el.classList.add(stateName);
          row.ico.innerHTML = icon({ active: "refresh", done: "check", fail: "alert" }[stateName] || "clock");
        };

        // 失败时把最后活跃的阶段标红，用户能看清停在哪一步
        let lastActiveIdx = -1;
        const onProgress = (p) => {
          const stage = p?.stage;
          const idx = stage === "done" ? stageDefs.length : stageIndex.get(stage);
          if (idx == null) return;
          lastActiveIdx = Math.min(idx, stageDefs.length - 1);
          rows.forEach((row, i) => setRowState(row, i < idx ? "done" : i === idx ? "active" : "pending"));
          if (Number(p.total) > 0 && idx < rows.length) rows[idx].count.textContent = `${p.current}/${p.total}`;
          if (p.message) msgLine.textContent = p.message;
          if (stage === "done") {
            errBanner.classList.remove("show");
            doneBtn.disabled = false;
            doneBtn.textContent = "完成";
            doneBtn.classList.add("btn-primary");
            doneBtn.onclick = () => close();
          }
        };
        offEvent = api.onEvent("storage:switch_progress", onProgress) || (() => {});

        body.append(
          h("div", { class: "acc-switch-steps" }, rows.map((r) => r.el)),
          msgLine,
          errBanner,
          h("div", { class: "acc-switch-note" }, "提交交接前可以取消；提交后中断会保留进度并继续恢复。"),
          h("div", { class: "acc-form-foot" }, doneBtn),
        );

        const showRetry = (result) => {
          if (lastActiveIdx >= 0) setRowState(rows[lastActiveIdx], "fail");
          errText.textContent = result?.message || "迁移暂未完成，可从当前进度重试。";
          errBanner.classList.add("show");
          doneBtn.disabled = false;
          doneBtn.textContent = "重试";
          doneBtn.classList.add("btn-primary");
          doneBtn.onclick = async () => {
            doneBtn.disabled = true;
            errBanner.classList.remove("show");
            await runAttempt(() => store.actions.resumeStorageMigration(result.transactionId));
          };
        };

        const runAttempt = async (operation) => {
          try {
            const result = await operation();
            if (result?.status === "paused") {
              const useLocal = await ui.confirm({
                message: result.message || "目标存储已有数据。请选择取消切换，或明确使用本地数据继续。",
                confirmText: "使用本地数据",
                cancelText: "取消切换",
              });
              if (!useLocal) {
                await store.actions.cancelStorageMigration(result.transactionId);
                close();
                toast("已取消存储切换", "info");
                return;
              }
              await runAttempt(() => store.actions.resumeStorageMigration(result.transactionId, "local"));
              return;
            }
            if (result?.status === "retryable") {
              if (needsRecoveryPassword(result.message)) {
                const configured = await promptRecoveryPassword();
                if (configured) {
                  errBanner.classList.remove("show");
                  msgLine.textContent = "恢复密码已设置，正在从已保存的进度继续迁移…";
                  doneBtn.disabled = false;
                  doneBtn.textContent = "重试";
                  doneBtn.onclick = async () => {
                    doneBtn.disabled = true;
                    await runAttempt(() => store.actions.resumeStorageMigration(result.transactionId));
                  };
                  return;
                }
              }
              showRetry(result);
              return;
            }
            rows.forEach((row) => setRowState(row, "done"));
            doneBtn.disabled = false;
            doneBtn.textContent = "完成";
            doneBtn.onclick = () => close();
            toast("存储方式已切换", "ok");
          } catch (e) {
            if (needsRecoveryPassword(errMsg(e))) {
              const configured = await promptRecoveryPassword();
              if (configured && !disposed) {
                await runAttempt(operation);
                return;
              }
            }
            if (lastActiveIdx >= 0) setRowState(rows[lastActiveIdx], "fail");
            errText.textContent = errMsg(e);
            errBanner.classList.add("show");
            doneBtn.disabled = false;
            doneBtn.textContent = "关闭";
            doneBtn.classList.remove("btn-primary");
            toast(`切换失败：${errMsg(e)}`, "err");
          }
        };

        runAttempt(() => store.actions.switchStorage(request));
      },
    });
  }

  /* ---------------- 票券卡 ---------------- */

  function ticket(acc) {
    const webdav = isWebdav(acc);
    const used = Math.max(Number(acc.usedBytes) || 0, 0);
    const ratio = Math.min(used / QUOTA_BYTES, 1);
    const meterTone = ratio >= 0.95 ? " danger" : ratio >= 0.8 ? " warn" : "";
    const busyVerify = verifying.has(acc.id);

    const switchInput = h("input", {
      type: "checkbox",
      checked: Boolean(acc.enabled),
      disabled: toggling.has(acc.id),
    });
    switchInput.addEventListener("change", () => onToggle(acc, switchInput.checked, switchInput));

    return h(
      "article",
      { class: `card card-hover acc-card${acc.enabled ? "" : " off"}${busyVerify ? " verifying" : ""}` },
      // 上半：账号名 + 角色/验证徽记 + 打码 Account ID
      h(
        "div",
        { class: "acc-top" },
        h(
          "div",
          { class: "acc-name-row" },
          h("h3", { class: "acc-name" }, webdav && acc.isPrimary ? "主空间" : acc.name || "未命名账号"),
          h(
            "div",
            { class: "acc-badges" },
            webdav ? h("span", { class: "badge info" }, "WebDAV") : h("span", { class: "badge mute" }, "Cloudflare"),
            acc.isPrimary
              ? h("span", { class: "badge info" }, iconEl("database"), webdav ? "目录索引中心" : "D1 索引中心")
              : h("span", { class: "badge mute" }, iconEl("hardDrive"), "存储节点"),
            verifyBadge(acc),
          ),
        ),
        // webdav 卡主行显示服务器 host，隐藏 Account ID
        webdav
          ? h(
              "div",
              { class: "acc-cid" },
              h("span", { class: "acc-cid-k" }, "服务器"),
              h("span", { class: "acc-cid-v" }, webdavHost(acc.webdavUrl)),
            )
          : h(
              "div",
              { class: "acc-cid" },
              h("span", { class: "acc-cid-k" }, "ACCOUNT ID"),
              h("span", { class: "acc-cid-v" }, maskId(acc.accountId)),
            ),
      ),
      // 撕票齿孔分隔线（签名视觉）
      h("div", { class: "acc-perf", "aria-hidden": "true" }),
      // 下半：存根信息
      h(
        "div",
        { class: "acc-body" },
        webdav
          ? h(
              "div",
              { class: "kv" },
              h("span", { class: "kv-k" }, "根目录"),
              h("span", { class: "kv-v mono" }, acc.webdavRoot || "GameSync"),
            )
          : h(
              "div",
              { class: "kv" },
              h("span", { class: "kv-k" }, "R2 Bucket"),
              h("span", { class: "kv-v mono" }, acc.r2Bucket || "未配置"),
            ),
        h(
          "div",
          { class: "kv" },
          h("span", { class: "kv-k" }, "已用空间"),
          // webdav 无固定免费额度，不标 10GB 分母
          h("span", { class: "kv-v" }, webdav ? fmtBytes(used) : `${fmtBytes(used)} / 10 GB`),
        ),
        webdav
          ? null
          : h(
              "div",
              { class: `acc-meter${meterTone}` },
              h("i", { style: { width: `${used > 0 ? Math.max(ratio * 100, 1.5) : 0}%` } }),
            ),
        h(
          "div",
          { class: "kv" },
          h("span", { class: "kv-k" }, "上次验证"),
          h("span", { class: "kv-v" }, fmtTime(acc.lastVerifiedAt)),
        ),
        acc.usageWarning ? h("div", { class: "acc-banner warn" }, iconEl("alert"), h("span", {}, acc.usageWarning)) : null,
        acc.lastError ? h("div", { class: "acc-banner err" }, iconEl("alert"), h("span", {}, acc.lastError)) : null,
      ),
      // 卡脚：启用开关 + 验证 / 编辑 / 删除
      h(
        "div",
        { class: "acc-foot" },
        h(
          "label",
          { class: "acc-toggle" },
          h("span", { class: "switch" }, switchInput),
          h("span", { class: "acc-toggle-text" }, acc.enabled ? "已启用" : "已停用"),
        ),
        h(
          "div",
          { class: "acc-actions" },
          h(
            "button",
            { class: `btn btn-sm${busyVerify ? " busy" : ""}`, onClick: () => onVerify(acc) },
            iconEl("refresh"),
            busyVerify ? "验证中" : "验证",
          ),
          h("button", { class: "btn btn-sm", onClick: () => openForm(acc) }, iconEl("pencil"), "编辑"),
          h(
            "button",
            { class: "btn btn-sm btn-danger", onClick: () => store.actions.deleteAccount(acc.id) },
            iconEl("trash"),
            webdav ? "断开" : "删除",
          ),
        ),
      ),
    );
  }

  /* ---------------- 页面渲染 ---------------- */

  function render() {
    const allAccounts = store.select.accounts();
    const primary = allAccounts.find((account) => account.isPrimary);
    const webdavMode = Boolean(primary && isWebdav(primary));
    const accounts = webdavMode ? [primary] : allAccounts;
    root.innerHTML = "";

    // 当前主账号：头部徽章展示存储方式，存在时才提供「切换存储方式」入口

    const page = h("div", { class: "page acc-page" });
    page.append(
      h(
        "header",
        { class: "acc-head" },
        h(
          "div",
          {},
          h("h1", { class: "acc-title" }, "云账号"),
          h(
            "p",
            { class: "acc-sub" },
            webdavMode
              ? "当前 WebDAV 地址与根目录共同构成唯一同步空间；更换空间请使用“切换存储方式”。"
              : "第一个账号为主账号，承载云端目录索引；其余为 Cloudflare 存储副账号。",
          ),
        ),
        h(
          "div",
          { class: "acc-head-actions" },
          primary
            ? h(
                "span",
                { class: "badge info" },
                iconEl(isWebdav(primary) ? "cloud" : "database"),
                `当前：${providerLabel(isWebdav(primary) ? "webdav" : "cloudflare")}`,
              )
            : null,
          primary
            ? h("button", { class: "btn", onClick: () => startSwitch() }, iconEl("refresh"), "切换存储方式")
            : null,
          webdavMode
            ? null
            : h(
                "button",
                { class: "btn btn-primary", onClick: () => openForm(null, ctx.params?.provider) },
                iconEl("plus"),
                "添加账号",
              ),
        ),
      ),
    );

    if (!accounts.length) {
      page.append(
        h(
          "div",
          { class: "empty" },
          h("div", { class: "empty-icon", html: icon("cloud") }),
          h("div", { class: "empty-title" }, "还没有云账号"),
          h(
            "div",
            { class: "empty-text" },
            "接入 Cloudflare 或 WebDAV 账号后，存档才能同步到云端。第一个账号将成为主账号，承载云端目录索引。",
          ),
          h(
            "button",
            { class: "btn btn-primary", onClick: () => openForm(null, ctx.params?.provider) },
            iconEl("plus"),
            "添加账号",
          ),
        ),
      );
      root.append(page);
      return;
    }

    const grid = h("div", { class: "acc-grid stagger" });
    for (const acc of accounts) grid.append(ticket(acc));
    page.append(grid);
    root.append(page);
  }

  render();
  // 渲染签名：账号数据没变时跳过重建（避免无关通知引发闪烁）
  let renderedSig = JSON.stringify(store.select.accounts());
  const off = store.subscribe(() => {
    const sig = JSON.stringify(store.select.accounts());
    if (sig === renderedSig) return;
    renderedSig = sig;
    render();
  });

  // welcome 页「配置云端存储」跳转过来时自动打开新建抽屉（provider 预选）
  if (ctx.params?.openNew) {
    window.setTimeout(() => {
      if (!disposed) openForm(null, ctx.params?.provider);
    }, 60);
  }

  return () => {
    disposed = true;
    off();
    activeDrawer?.close();
    activeSwitchDialog?.close();
  };
}
