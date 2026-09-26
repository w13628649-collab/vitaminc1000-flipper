// 精炼页(设计 §4.5):原料 → 半成品,回答两件事 ——
//   ① 原料和下级料怎么拿最便宜:直接买、挂买单收、从低级转化上来、下级料买现成还是自己炼;
//   ② 照这样做出来卖掉赚多少,前 N 条路线并排比。
//
// 和 app.js 共用全局作用域:$ / esc / num / pct / getJSON / rt / rtSetKeys / NAMES / noteName /
// refineRole / refineID / roleTag / viewHooks / DEFAULT_CITIES … 都是那边定义的,这个文件在它之后加载。
//
// 接口:POST /api/refine/plan(一个目标的全部候选)、GET /api/refine/matrix(一条线 25 档)、
// GET /api/refine/recipe(配方树:收集实时订阅的 key、给校准提供基础值)。
// 个人参数(专精、今日专注、站费、k)只存本机 localStorage,每次请求带上;没填的字段不带,
// 服务端用它自己的默认值 —— 默认值是多少,响应里的 server_defaults 原样告诉界面。
"use strict";

const CRAFT_LS = "craft.v1";
const CRAFT_TIERS = [4, 5, 6, 7, 8];
const CRAFT_ENCH = [0, 1, 2, 3, 4];
const CRAFT_TOP = 12;
// 服务端还没回过话时的兜底(和 conf.DefaultRefining 一致);回过话以后一律用 server_defaults
const CRAFT_FALLBACK = { focus_cap: 30000, window_hours: 72 };
const craftLine = key => REFINE_FAMILIES.find(f => f.craft && f.line === key) || null;

const craft = {
  // p 是存进本机的全部个人参数。null / 空串 = 没填,用服务端默认
  p: {
    line: "ORE", tier: 8, ench: 4, qty: 10,
    cityBy: {},                 // 线 → 生产城市;没填 = 这条线的专精城
    focusMode: "auto", customTiers: [8], budget: null,
    sell: "post", sort: "daily",
    fce: {},                    // 线 → { tier: FCE }:专精按线算,矿和皮是两棵树
    unlocked: {},               // 线 → 精炼已解锁到 T 几
    station: {},                // 城市 → 精炼站费(每个城的站是不同玩家的)
    tstation: {},               // 城市 → 转化主人费率
    k: null, kAt: "", kSrc: "", kGold: null,   // 转化系数 k、录入日期、怎么来的(calib 校准 / manual 手填)、那时的金价
    bonus: {},                  // 线 → { v, d }:今日生产加成和填的那天。过了那天就不算(每天变)
    island: false, sellCity: "", srcCities: [], transport: null,
    bid: false, bidMax: null, leftover: "", maxAge: null, capital: null, share: null, gold: null,
    heart: "",                  // 心也参与返还:"" 服务端默认 | "1" 返还 | "0" 不返还
    // 待核实参数哪天填的:「station:城市」「tstation:城市」「fce:线」「unlocked:线」「heart」→ 日期。
    // 填了就算核对过(请求里带 confirmed),哪怕填的正好是默认值 —— 专精真是 0、站费真是 500 的人,
    // 以前「未核实」的标永远去不掉。k 和生产加成自己带日期(kAt、bonus[线].d)
    confAt: {},
  },
  sd: null,          // 服务端默认值(响应里的 server_defaults)
  data: null,        // 最近一次 plan 响应
  sig: "",           // 最近一次请求体:同样的参数短时间内不重复请求
  loadedAt: 0, seq: 0, pending: false, timer: 0, retry: 0, err: null,
  open: new Set(),   // 候选表里展开着的行(按候选 id)
  rawAll: new Set(), // 原料来路里「显示全部起点」的那几种原料
  rawOpen: false,    // 「下级链自炼用到的原料」展开着没有(重算、实时刷新后照旧)
  keys: new Set(),   // 实时订阅的盘口 key(配方树上的全部物品,见 craftKeysOf)
  liveTimer: 0, liveFirst: 0, liveQuote: null, liveNote: "",
  items: new Map(),  // 物品 id → { zh, en }:按物品族一次拉全,精炼页的名字都从这来
  families: new Set(),
};

// ── 本机存储:读写都 try/catch(隐私模式、存储被禁时 localStorage 会抛) ──
function craftRestore() {
  let saved = null;
  try { saved = JSON.parse(localStorage.getItem(CRAFT_LS) || "null"); } catch (e) { saved = null; }
  if (!saved || typeof saved !== "object") return;
  const def = craft.p;
  for (const k of Object.keys(def)) {
    const v = saved[k], d = def[k];
    if (v === undefined) continue;
    // 类型对不上的(老版本存的、手改坏的)不收,保持默认
    if (d === null ? (v === null || typeof v === "number") :
      Array.isArray(d) ? Array.isArray(v) :
      typeof d === "object" ? (v && typeof v === "object" && !Array.isArray(v)) :
      typeof v === typeof d) def[k] = v;
  }
  if (!craftLine(def.line)) def.line = "ORE";
  if (!CRAFT_TIERS.includes(def.tier)) def.tier = 8;
  if (!CRAFT_ENCH.includes(def.ench)) def.ench = 4;
  if (!(Number.isInteger(def.qty) && def.qty >= 1 && def.qty <= 10000)) def.qty = 10;
  def.customTiers = def.customTiers.filter(t => CRAFT_TIERS.includes(t));
  def.srcCities = def.srcCities.filter(c => typeof c === "string");
}
function craftSave() {
  try { localStorage.setItem(CRAFT_LS, JSON.stringify(craft.p)); } catch (e) { /* 存不了就只在这一页有效 */ }
}
craftRestore();

// ── 名字和小工具 ──
const cLine = () => craftLine(craft.p.line) || craftLine("ORE");
const cTarget = () => refineID(cLine().refined, craft.p.tier, craft.p.ench);
const cCity = () => craft.p.cityBy[craft.p.line] || cLine().city;
const cFCE = () => craft.p.fce[craft.p.line] || {};
const cToday = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};
// 今日生产加成:只认今天填的,昨天填的当没填(游戏里每天换品类)
function cBonus() {
  const b = craft.p.bonus[craft.p.line];
  return b && b.d === cToday() && typeof b.v === "number" ? b.v : null;
}
// 记下 / 清掉某个待核实参数是哪天填的(p.confAt 的键见上)
function cMark(key, on) {
  if (on) craft.p.confAt[key] = cToday();
  else delete craft.p.confAt[key];
}
// 专精填全了没有:目标 tier 及以下每一级都填了(0 也算填了)。只填一部分不算核对过
const cFCEFull = () => { const f = cFCE(); return CRAFT_TIERS.filter(t => t <= craft.p.tier).every(t => typeof f[t] === "number"); };
// 用户填过的待核实参数(UnverifiedParams 的键)。请求里带上,服务端就不再把它们标成「未核实」
function craftConfirmed() {
  const p = craft.p, city = cCity(), out = [];
  if (p.k != null) out.push("fees.transmute_factor");
  if (cFCEFull()) out.push("focus.fce");
  if (p.unlocked[p.line] != null) out.push("focus.unlocked_tier");
  if (p.station[city] != null) out.push("fees.station_per100");
  if (p.tstation[city] != null) out.push("fees.transmute_station_per100");
  if (cBonus() != null) out.push("rrr.daily_bonus");
  if (p.heart) out.push("heart_returns");
  return out;
}
// 某个待核实参数是哪天填的(显示用);没填是 ""
function cConfDate(key) {
  const p = craft.p, city = cCity(), line = p.line;
  switch (key) {
    case "fees.transmute_factor": return p.k != null ? p.kAt || "" : "";
    case "focus.fce": return p.confAt[`fce:${line}`] || "";
    case "focus.unlocked_tier": return p.confAt[`unlocked:${line}`] || "";
    case "fees.station_per100": return p.confAt[`station:${city}`] || "";
    case "fees.transmute_station_per100": return p.confAt[`tstation:${city}`] || "";
    case "rrr.daily_bonus": return cBonus() != null ? cToday() : "";
    case "heart_returns": return p.confAt.heart || "";
  }
  return "";
}
// 精炼页能用的城市:扫描覆盖率里的(就是服务端配置的城市),没有就用默认那 7 座
function craftCities() {
  const out = [];
  for (const c of scan?.coverage || []) if (c.city && c.city !== "Black Market" && !out.includes(c.city)) out.push(c.city);
  if (!out.length) out.push(...DEFAULT_CITIES);
  return out;
}
// T8_ORE_LEVEL4@4 → T8.4;T3_METALBAR → T3;心没有 tier
function cTier(id) {
  const m = REFINE_ID_RE.exec(String(id || ""));
  if (!m) return "";
  return Number(m[1]) >= 4 ? `T${m[1]}.${m[3] || 0}` : `T${m[1]}`;
}
function cName(id) {
  const it = craft.items.get(id);
  if (it?.zh) return it.zh;
  const n = NAMES.get(id);
  return n ? n.replace(/\.\d$/, "") : "";
}
// 「T8.4 精纯亚德曼钢条」。心这种不按 tier 编号的只给名字
const cLabel = id => [cTier(id), cName(id) || (cTier(id) ? "" : id)].filter(Boolean).join(" ");
const HEART_WORD = "心";
function cRoleTag(id) {
  const r = refineRole(id);
  if (r) return roleTag(r);
  return /_FACTION_.*_TOKEN_/.test(String(id)) ? `<span class="rtag heart" title="派系心:.0~.3 的心配方要用">${HEART_WORD}</span>` : "";
}
// 精炼页里的物品名:点了跳查价页(和机会表的物品名一个规矩)
const cItem = (id, opts = {}) =>
  `<a class="citem" href="#lookup/${encodeURIComponent(id)}" title="${esc(id)} · 点开查价">${esc(cLabel(id))}</a>${opts.noTag ? "" : cRoleTag(id)}`;
// 件数:稳态消耗是小数(按期望返还算),整数就不带小数点;上百件的小数没有意义,取整
const fq = q => (q == null || !isFinite(q) ? "—"
  : Math.abs(q) >= 100 || Math.abs(q - Math.round(q)) < 0.005 ? num(q) : q.toFixed(2));
const sgn = n => (n == null || !isFinite(n) ? "—" : (n > 0 ? "+" : n < 0 ? "−" : "") + num(Math.abs(n)));
// 可能是负数、但不需要 + 号的数(每点专注值):负号统一用 −
const snum = n => (n == null || !isFinite(n) ? "—" : (n < 0 ? "−" : "") + num(Math.abs(n)));
// 带正负号的百分比(利润率、和直接买比贵多少):减号和 sgn 一样用 −
const spct = (n, d = 1) => (n == null || !isFinite(n) ? "—" : (n > 0 ? "+" : n < 0 ? "−" : "") + (Math.abs(n) * 100).toFixed(d) + "%");
const hms = t => {
  const d = t instanceof Date ? t : new Date(t);
  return isFinite(d) ? d.toLocaleTimeString("zh-CN", { hour12: false }) : "—";
};
// 可能是前一两天的时刻(串城检测覆盖到多早):带上月-日
const dhm = t => {
  const d = t instanceof Date ? t : new Date(t);
  return isFinite(d) ? `${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")} ${
    d.toLocaleTimeString("zh-CN", { hour12: false, hour: "2-digit", minute: "2-digit" })}` : "—";
};
// 价格年龄:超过 6 小时黄、超过 24 小时红(和引擎的 age_level 同一条线)
function ageTag(h, title) {
  if (h == null || !isFinite(h)) return `<span class="agetag" title="没有观测时间">—</span>`;
  const lv = h > 24 ? "red" : h > 6 ? "yellow" : "ok";
  const t = title || (lv === "red" ? "超过 24 小时:这个价多半已经变了" : lv === "yellow" ? "超过 6 小时:进游戏看一眼再下单" : "6 小时以内");
  return `<span class="agetag ${lv}" title="${esc(t)}">${h < 1 ? Math.max(1, Math.round(h * 60)) + " 分钟" : h.toFixed(1) + " 小时"}</span>`;
}
const SRC_NAME = { capture: "抓包", aodp: "AODP" };

// 警告代码 → 短标签(引擎 refine/plan.go 和服务层 flip/refine.go 的全部代码)。没列的原样显示代码
const CRAFT_WARN = {
  depth_short: "深度不够", tail_short: "余料备不齐", missing: "缺价", truncated: "盘口没翻完",
  depth_unknown: "深度未知", unverified: "参数未核实", thin_volume: "成交量薄", no_volume: "没有成交量",
  bid_fill_risk: "挂买单收不收得到?", over_focus_budget: "专注不够", stale: "价格旧", sell_cliff: "买单断崖",
  no_sell_price: "没有卖价", locked: "没解锁", qty_bound: "卡在批量", capital_short: "本金不够",
  aodp_newer: "AODP 更新", aodp_unavailable: "AODP 没取到", conflicted: "串过城",
};
// 这几种是「数不能全信」,标红;其余是提醒
const CRAFT_WARN_BAD = new Set(["depth_short", "missing", "no_sell_price", "locked", "capital_short", "stale", "conflicted"]);
// 服务端的警告文字里是物品 id(T8_ORE_LEVEL4@4 缺价…):换成页面上别处一样的中文名。
// 只查到 tier、没查到名字的保留 id,免得 T3_METALBAR 变成光秃秃的「T3」
const ITEM_ID_RE = /\bT\d_[A-Z0-9_]+(?:@\d)?/g;
const idLabel = id => { const l = cLabel(id); return l && l !== cTier(id) ? l : id; };
const warnText = msg => String(msg || "").replace(ITEM_ID_RE, idLabel);
function warnHTML(msg) {
  const s = String(msg || "");
  let out = "", last = 0;
  for (const m of s.matchAll(ITEM_ID_RE)) {
    out += esc(s.slice(last, m.index)) + `<span class="wid" title="${esc(m[0])}">${esc(idLabel(m[0]))}</span>`;
    last = m.index + m[0].length;
  }
  return out + esc(s.slice(last));
}
const warnTag = w => `<span class="wtag${CRAFT_WARN_BAD.has(w.code) ? " bad" : ""}" title="${esc(warnText(w.msg || w.code))}">${
  esc(CRAFT_WARN[w.code] || w.code)}</span>`;
function warnList(ws) {
  if (!ws?.length) return "";
  return `<ul class="c-warns">${ws.map(w => `<li>${warnTag(w)} ${warnHTML(w.msg)}</li>`).join("")}</ul>`;
}

// 按物品族一次拉全名字(/api/items?family=METALBAR 是 T2~T8 全部附魔,二十几件)。
// 心不按族编号,单独按 id 查
async function craftNames(line) {
  const L = craftLine(line);
  if (!L) return;
  const fams = [L.raw, L.refined].filter(f => !craft.families.has(f));
  if (!fams.length) return;
  await Promise.all(fams.map(async fam => {
    try {
      const rows = await getJSON(`/api/items?limit=300&family=${encodeURIComponent(fam)}`);
      for (const it of rows || []) {
        craft.items.set(it.item_id, { zh: it.name_zh || it.name_en || "", en: it.name_en || "" });
        noteName(it.item_id, displayName(it));
      }
      craft.families.add(fam);
    } catch (e) { /* 目录还没同步:先显示 T8.4 这种短名,下次再拉 */ }
  }));
}

// ── 参数条 ──
function craftRenderGrid() {
  const L = cLine(), p = craft.p;
  const head = `<span></span>${CRAFT_ENCH.map(e => `<span class="th">.${e}</span>`).join("")}`;
  const rows = CRAFT_TIERS.map(t => `<span class="th">T${t}</span>${CRAFT_ENCH.map(e => {
    const id = refineID(L.refined, t, e), on = t === p.tier && e === p.ench;
    const name = cName(id);
    return `<button data-t="${t}" data-e="${e}" class="${on ? "on" : ""}" aria-pressed="${on}"
      title="${esc(`${cLabel(id)}(半成品)· ${id}`)}"><b>${t}.${e}</b><i>${esc(name || "—")}</i></button>`;
  }).join("")}`).join("");
  $("c-grid").innerHTML = head + rows;
  const id = cTarget();
  $("c-target-name").innerHTML = `${esc(cLabel(id))} ${roleTag(refineRole(id))}`;
  for (const b of $("c-line").querySelectorAll("button")) b.setAttribute("aria-pressed", String(b.dataset.line === p.line));
  for (const b of $("c-sell").querySelectorAll("button")) b.setAttribute("aria-pressed", String(b.dataset.sell === p.sell));
}

const cSetOptions = (sel, opts, value) => {
  const html = opts.map(([v, t]) => `<option value="${esc(v)}">${esc(t)}</option>`).join("");
  if (sel.dataset.sig !== html) { sel.innerHTML = html; sel.dataset.sig = html; }
  sel.value = value;
};
// 把状态写回表单。正在打字的那个框不动,免得光标跳走;占位符是服务端默认值
function craftSyncForm() {
  const p = craft.p, sd = craft.sd || {}, city = cCity(), L = cLine();
  const focused = document.activeElement;
  const setVal = (id, v) => { const el = $(id); if (el !== focused) el.value = v == null ? "" : String(v); };
  const setPh = (id, v) => { $(id).placeholder = v == null || v === "" ? "服务端默认" : String(v); };
  const cities = craftCities();
  cSetOptions($("c-city"), cities.map(c => [c, c === L.city ? `${c}(专精城)` : c]), city);
  setVal("c-qty", p.qty);
  $("c-focus").value = p.focusMode;
  $("c-custom-wrap").hidden = p.focusMode !== "custom";
  $("c-custom").innerHTML = CRAFT_TIERS.filter(t => t <= p.tier).reverse().map(t =>
    `<label><input type="checkbox" data-ct="${t}"${p.customTiers.includes(t) ? " checked" : ""}> T${t}</label>`).join("");
  setVal("c-budget", p.budget);
  setPh("c-budget", sd.focus_per_day);
  $("c-budget").max = String(sd.focus_cap || CRAFT_FALLBACK.focus_cap);
  $("c-sort").value = p.sort;

  // 高级
  $("c-fce-line").textContent = `(${L.line === "ORE" ? "矿石" : L.line === "HIDE" ? "兽皮" : L.line === "FIBER" ? "纤维" : "木材"}精炼,按 tier)`;
  const fce = cFCE();
  if (!$("c-fce").children.length) {
    $("c-fce").innerHTML = CRAFT_TIERS.map(t => `<div class="ctrl"><label for="c-fce-${t}">T${t}</label>
      <input type="number" id="c-fce-${t}" data-fce="${t}" min="0" max="40000" step="100" placeholder="0"></div>`).join("");
  }
  for (const t of CRAFT_TIERS) setVal(`c-fce-${t}`, fce[t] || fce[t] === 0 ? fce[t] : null);
  // 「服务端默认」和「T8(全部)」分开:选了 T8 才算核对过
  $("c-unlock").value = p.unlocked[p.line] == null ? "" : String(p.unlocked[p.line]);
  const heartDef = craft.data?.params_used && !p.heart ? craft.data.params_used.heart_returns : null;
  $("c-heart").options[0].textContent = `服务端默认${heartDef == null ? "" : heartDef ? "(返还)" : "(不返还)"}`;
  $("c-heart").value = p.heart || "";
  setVal("c-station", p.station[city]);
  setPh("c-station", sd.station_fee_by_city?.[city] ?? sd.station_fee_per100);
  setVal("c-k", p.k);
  setPh("c-k", sd.transmute_factor);
  setVal("c-tstation", p.tstation[city]);
  setPh("c-tstation", sd.transmute_station_fee_per100);
  const bonus = cBonus();
  $("c-bonus").options[0].textContent = `服务端默认(+${num(sd.daily_bonus?.[L.line] ?? 0)})`;
  $("c-bonus").value = bonus == null ? "" : String(bonus);
  $("c-island").checked = !!p.island;
  cSetOptions($("c-sellcity"), [["", `同生产城市(${city})`], ...cities.filter(c => c !== city).map(c => [c, c])],
    p.sellCity && p.sellCity !== city ? p.sellCity : "");
  $("c-src").innerHTML = cities.filter(c => c !== city).map(c =>
    `<label><input type="checkbox" data-src="${esc(c)}"${p.srcCities.includes(c) ? " checked" : ""}> ${esc(c)}</label>`).join("");
  setVal("c-transport", p.transport);
  setPh("c-transport", 0);
  $("c-bid").checked = !!p.bid;
  setVal("c-bidmax", p.bidMax);
  setPh("c-bidmax", 0);
  $("c-left").value = p.leftover || "";
  setVal("c-maxage", p.maxAge);
  setPh("c-maxage", sd.window_hours);
  $("c-maxage").max = String(sd.window_hours || CRAFT_FALLBACK.window_hours);
  setVal("c-capital", p.capital);
  setPh("c-capital", sd.capital);
  setVal("c-share", p.share);
  // 成交份额的服务端默认不在 server_defaults 里,从上一次实际用的参数看(没填时就是服务端默认)
  const share = p.share == null ? craft.data?.params_used?.daily?.volume_share : null;
  setPh("c-share", share != null ? Math.round(share * 100) : null);
  setVal("c-gold", p.gold);
  setPh("c-gold", craftGold());
  craftBadges();
}

// 金价:只用来画「为专注养 premium 小号」的门槛线(D6)。服务端配置里记了量 k 那天的金价就用它,
// 没有就用 2026-09-26 AODP 的金价;用户可以自己改
const CRAFT_GOLD_FALLBACK = 13554;
const craftGold = () => craft.p.gold || craft.sd?.transmute_gold_price || CRAFT_GOLD_FALLBACK;
// premium 30 天 3750 金、每天恢复 10000 专注:每点专注的成本 = 3750 × 金价 ÷ 300000(refine.PremiumSilverPerFocus)
const craftPremiumSPF = () => 3750 * craftGold() / 300000;

// 「未核实」徽章:k、站费、主人费率、生产加成。k 另带录入日期(设计 §4.5)
function craftBadges() {
  const p = craft.p, sd = craft.sd || {}, city = cCity();
  const badge = (id, set, text, title) => {
    const el = $(id);
    el.className = "ubadge" + (set ? " mine" : "");
    el.textContent = text;
    el.title = title;
  };
  if (p.k != null) {
    const how = p.kSrc === "calib" ? "按转化明细算的" : "手填的";
    badge("c-k-badge", true, `${p.kSrc === "calib" ? "实测" : "手填"} · ${p.kAt || "日期不明"}`,
      `${how} k=${p.k}${p.kGold ? `,当时金价 ${num(p.kGold)}` : ""}。k 随时间变过,隔一阵再量一次`);
  } else {
    badge("c-k-badge", false, `未核实 · 录入 ${sd.transmute_measured_at || "日期不明"}`,
      `服务端默认 k=${sd.transmute_factor ?? "—"},是从亚服表格反推的,没在游戏里核对过。用下面「校准」量一次`);
  }
  // 填了就是「你填的 · 日期」:填的值正好等于默认值也一样(那正是核对过的意思)
  const filled = key => `你填的 · ${cConfDate(key) || "日期不明"}`;
  const st = p.station[city];
  badge("c-station-badge", st != null, st != null ? filled("fees.station_per100") : "未核实",
    st != null ? `${city} 的精炼站费按你填的 ${st} 算` : "服务端默认的站费是表格作者用的数,没核实。进城按 N 看地图,悬停在精炼站上");
  const ts = p.tstation[city];
  badge("c-tstation-badge", ts != null, ts != null ? filled("fees.transmute_station_per100") : "未核实",
    ts != null ? `${city} 的转化主人费率按你填的 ${ts} 算` : "服务端默认值,没核实。校准里填主人费用可以反推");
  const b = cBonus(), stale = craft.p.bonus[craft.p.line] && b == null;
  badge("c-bonus-badge", b != null, b != null ? `今天 · ${cToday()}` : stale ? "昨天填的,已作废" : "未核实",
    "每日生产加成每天换品类,只能看游戏里的「活动」窗口手填;只对填的那一天有效");
  const ul = p.unlocked[p.line];
  badge("c-unlock-badge", ul != null, ul != null ? filled("focus.unlocked_tier") : "未核实",
    ul != null ? `按你选的:精炼解锁到 T${ul}` : "服务端默认按 T8 全解锁算。命运之轮里看 T8 精炼节点解锁没有,选一下就算核对过");
  const fceKeys = Object.keys(cFCE()).length;
  badge("c-fce-badge", cFCEFull(), cFCEFull() ? filled("focus.fce") : fceKeys ? "只填了一部分" : "未核实",
    cFCEFull() ? "这条线的专精按你填的算(0 也是填了)"
      : `T4~T${p.tier} 每一级都填了才算核对过;没填的按 0 算。没点过这条线的专精就点「全填 0」`);
  badge("c-heart-badge", !!p.heart, p.heart ? filled("heart_returns") : "未核实",
    p.heart ? `按你选的:心${p.heart === "1" ? "" : "不"}参与返还` : "只影响用心的那几条配方(.4 没有心配方);用心、开专注精炼一小批,看有没有返还心");
  // 高级区的摘要:折起来也看得到改过哪些
  const mine = [];
  if (fceKeys) mine.push("专精");
  if (p.k != null) mine.push("k");
  if (ul != null) mine.push("解锁");
  if (st != null) mine.push("站费");
  if (ts != null) mine.push("主人费率");
  if (b != null) mine.push("生产加成");
  if (p.heart) mine.push("心返还");
  if (p.island) mine.push("个人岛");
  if (p.srcCities.length) mine.push("跨城买料");
  if (p.bid) mine.push("挂买单收料");
  if (p.leftover) mine.push("余料估值");
  if (p.capital != null) mine.push("本金");
  $("c-adv-sum").textContent = mine.length ? `你改过:${mine.join("、")}` : "全部用服务端默认";
  craftCalSummary();
}

// ── 请求体:只带用户填了的字段,没填的让服务端用默认值 ──
function craftBody() {
  const p = craft.p, city = cCity();
  const body = {
    target: cTarget(), city, qty: p.qty, sort: p.sort, top: CRAFT_TOP,
    sell: { modes: p.sell === "take" ? ["take", "post"] : ["post", "take"], city: p.sellCity || city },
    focus: { mode: p.focusMode, fce: {} },
    sources: { cities: [city, ...p.srcCities.filter(c => c !== city)], bid_orders: !!p.bid },
    rrr: { island: !!p.island }, fees: {}, daily: {},
  };
  if (p.focusMode === "custom") body.focus.custom_tiers = p.customTiers.filter(t => t >= 4 && t <= p.tier);
  if (p.budget != null) body.focus.budget = p.budget;
  const fce = cFCE();
  for (const t of CRAFT_TIERS) body.focus.fce[t] = Number(fce[t]) || 0;
  if (p.unlocked[p.line] != null) body.focus.unlocked_tier = p.unlocked[p.line];
  if (p.transport != null) body.sources.transport_pct = p.transport / 100;
  if (p.bidMax != null) body.sources.bid_max_qty = p.bidMax;
  if (p.maxAge != null) body.sources.max_age_hours = p.maxAge;
  if (p.station[city] != null) body.fees.station_per100 = p.station[city];
  if (p.tstation[city] != null) body.fees.transmute_station_per100 = p.tstation[city];
  if (p.k != null) body.fees.transmute_factor = p.k;
  const b = cBonus();
  if (b != null) body.rrr.daily_bonus = b;
  if (p.leftover) body.leftover = p.leftover;
  if (p.capital != null) body.daily.capital = p.capital;
  if (p.share != null) body.daily.volume_share = p.share / 100;
  if (p.heart) body.heart_returns = p.heart === "1";
  const confirmed = craftConfirmed();
  if (confirmed.length) body.confirmed = confirmed;
  return body;
}

// ── 计算 ──
// opts.force:参数没变也重新请求(重算按钮、实时推送、切回本页);opts.live:实时触发的,旧结果不变灰
async function craftLoad(opts = {}) {
  clearTimeout(craft.timer);
  clearTimeout(craft.retry);
  const body = craftBody();
  const sig = JSON.stringify(body);
  if (!opts.force && sig === craft.sig && (craft.pending || Date.now() - craft.loadedAt < 20000)) return;
  craft.sig = sig;
  const seq = ++craft.seq;
  craft.pending = true;
  craft.err = null;
  if (!opts.live) $("c-result").classList.add("dim");
  craftStatus(opts.live ? "live" : "loading");
  try {
    const res = await getJSON("/api/refine/plan", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: sig,
    });
    if (seq !== craft.seq) return;   // 连着改参数时,先发的请求晚到不能盖掉后发的
    craft.pending = false;
    const prev = craft.data;
    craft.data = res;
    craft.loadedAt = Date.now();
    if (res.server_defaults) craft.sd = res.server_defaults;
    craft.tries = 0;
    craftSyncForm();
    craftRender(res);
    craftLiveAfter(res, opts);
    craftStatus("ok", opts);
    craftAfterPlan(res);
    // 实时重算后推荐那条的单根成本变了就闪一下:涨红跌绿(成本涨是坏事)
    if (opts.live && prev?.target === res.target) {
      const a = prev.candidates?.find(c => !c.reference), b = res.candidates?.find(c => !c.reference);
      const el = $("c-result").querySelector(".c-kpis > div");
      if (a && b && el && Math.abs(a.unit_cost - b.unit_cost) > 0.5) flash(el, b.unit_cost < a.unit_cost ? "up" : "down");
    }
  } catch (e) {
    if (seq !== craft.seq) return;
    craft.pending = false;
    craft.sig = "";   // 失败了:同样的参数下次还要能再请求
    craft.err = e;
    $("c-result").classList.remove("dim");
    // 504 = 服务端还在等 AODP 限流,计算在后台继续;过几秒再要一次就有了。最多自动再试两次
    if (e.status === 504 && (craft.tries = (craft.tries || 0) + 1) <= 2) {
      craft.retry = setTimeout(() => craftLoad({ force: true }), 4000);
    }
    craftStatus("error");
  }
}
// 参数一变就算,但攒 350 毫秒:连着点网格、打字时不用每下都打一次服务端
function craftChanged(opts = {}) {
  craftSave();
  if (opts.grid) craftRenderGrid();
  craftSyncForm();
  clearTimeout(craft.timer);
  craft.timer = setTimeout(() => craftLoad(), opts.now ? 0 : 350);
  if (currentView === "craft" && opts.grid) history.replaceState(null, "", "#" + viewHooks.craft.hash());
  // 校准开着时,换了线 / 目标就按新的配方树重画(基础专注、转化格子都跟着变)
  if (opts.grid && $("c-cal").open) craftRenderCal();
  // 矩阵开着就跟着参数重算(和目标无关的参数没变时查询串一样,不会重复请求)
  clearTimeout(mx.timer);
  mx.timer = setTimeout(() => craftMatrixLoad(), 600);
}
// ── 实时:把配方树上所有物品的盘口交给统一的 WS 模块(设计 §4.5「实时」) ──
// WS 只推最优价,所以它只当「触发器」:命中这些 key 就防抖 1.5 秒、整份计划找服务端重算,
// 阶梯以服务端为准。订阅不要快照(snapshot:false):计划本身就是整份拉的
const CRAFT_LIVE_MS = 1500;      // 同一波推送攒 1.5 秒再重算(一次翻页会连着推好几条)
const CRAFT_LIVE_MAX_MS = 5000;  // 推送一直不停时最多攒 5 秒,不能一直不算
const cUniq = xs => [...new Set(xs.filter(Boolean))];
// 要订的盘口 = 服务端读的那一批(flip/refine.go 的 refineKeys):配方树上的物品(链上每一级、
// 每级配方里的原料和心、原料的全部转化起点)× 买料城市 × 两边,加上目标在出售城市的两边
function craftKeysOf(res, tree) {
  const items = new Set([res.target]);
  for (const n of tree.chain || []) {
    items.add(n.item);
    if (!n.makeable) continue;
    for (const r of n.recipes || []) for (const i of r.inputs || []) items.add(i.item);
  }
  for (const r of tree.raws || []) for (const rt of r.routes || []) items.add(rt.start);
  const pu = res.params_used || {};
  const buy = cUniq([res.city, ...(pu.sources?.cities || [])]);
  const keys = [];
  for (const it of items) for (const c of buy) for (const s of [0, 1]) keys.push(`${it}|${c}|1|${s}`);
  const sellCity = pu.sell?.city || res.city;
  for (const s of [0, 1]) keys.push(`${res.target}|${sellCity}|1|${s}`);
  return cUniq(keys);
}
async function craftAfterPlan(res) {
  let tree;
  try { tree = await craftTree(res.target); } catch (e) { return; }   // 配方树没拉到:这一轮不订,下次算完再试
  if (craft.data !== res) return;   // 等配方树的时候又算了一份新的
  craft.keys = new Set(craftKeysOf(res, tree));
  if (currentView === "craft") rtSetKeys("craft", [...craft.keys], { snapshot: false });
  craftStatus("ok");
  // 心不按物品族编号,按族拉名字拉不到:单独按 id 查一次,查到就重画
  const hearts = [...new Set((tree.chain || []).flatMap(n => (n.recipes || []).flatMap(r => (r.inputs || [])
    .filter(i => i.kind === "heart").map(i => i.item))))].filter(id => !cName(id));
  if (hearts.length && await resolveNames(hearts) && craft.data === res) craftRender(res);
}
function onCraftQuote(q, pushed) {
  // 快照不触发(那是刚订阅时补的);不在这页时已经退订,晚到的也不管
  if (!pushed || currentView !== "craft" || !craft.data || !craft.keys.has(q.k)) return;
  craft.liveQuote = q;
  const now = Date.now();
  if (!craft.liveFirst) craft.liveFirst = now;
  clearTimeout(craft.liveTimer);
  const wait = Math.max(0, Math.min(CRAFT_LIVE_MS, craft.liveFirst + CRAFT_LIVE_MAX_MS - now));
  craft.liveTimer = setTimeout(() => craftLiveKick("quote"), wait);
}
function craftLiveKick(why) {
  clearTimeout(craft.liveTimer);
  craft.liveTimer = 0;
  craft.liveFirst = 0;
  if (currentView !== "craft" || !craft.data) return;
  // 正在算(用户刚改了参数):那一份算完就是新的,不用再补
  if (craft.pending) return;
  const q = why === "quote" ? craft.liveQuote : null;
  craft.liveQuote = null;
  craftLoad({ force: true, live: true, why, quote: q });
}
rt.quoteFns.push(onCraftQuote);
// 断线重连:断开那段时间的推送收不到了,补拉一次(第一次连上不用,那时刚算过)
rt.openFns.push(() => { if (rt.opens > 1 && craft.data && currentView === "craft") craftLiveKick("reconnect"); });
// 服务端说这条连接丢过消息:丢的是哪几条不知道,整份重算一次
rt.resyncFns.push(() => { if (craft.data && currentView === "craft") craftLiveKick("resync"); });
const cKeyText = k => {
  const [item, city, , side] = String(k).split("|");
  return `${cLabel(item) || item} @ ${city} ${side === "0" ? "卖单" : "买单"}`;
};
// 拿到计划之后:实时的说明文字、防止拿到服务端 2 秒缓存里的旧计划
function craftLiveAfter(res, opts) {
  if (!opts.live) { craft.liveNote = ""; return; }
  const q = opts.quote;
  craft.liveNote = q ? `已按 ${hms(q.t)} 报价重算(${cKeyText(q.k)})`
    : opts.why === "reconnect" ? `断线重连后已补拉(${hms(new Date())})`
    : opts.why === "resync" ? `推送丢过消息,已整份重算(${hms(new Date())})`
    : `已重算(${hms(new Date())})`;
  // 服务端同样的参数 2 秒内回缓存:读簿时刻早于这条报价,说明拿到的是报价之前算的那份,过一会儿再要一次
  const readAt = Date.parse(res.books?.read_at || ""), qt = Date.parse(q?.t || "");
  if (q && isFinite(readAt) && isFinite(qt) && readAt < qt && !opts.retried) {
    craft.liveNote = `收到 ${hms(q.t)} 的报价,服务端还是旧的一份,马上再算一次…`;
    setTimeout(() => { if (!craft.pending) craftLoad({ ...opts, retried: true }); }, 2200);
  }
}

function craftStatus(state, opts = {}) {
  const el = $("c-status");
  const d = craft.data;
  if (state === "loading") {
    el.className = "c-status busy";
    el.innerHTML = `正在算 ${esc(cLabel(cTarget()))} @ ${esc(cCity())} × ${num(craft.p.qty)} …(读盘口、必要时等 AODP,慢的时候十几秒)`;
    return;
  }
  if (state === "live") {
    el.className = "c-status busy";
    el.innerHTML = `报价变了,正在按新盘口重算 …`;
    return;
  }
  if (state === "error") {
    const e = craft.err;
    el.className = "c-status bad";
    el.innerHTML = e?.status === 400
      ? `<b>参数不对:</b>${esc(e.message)}`
      : e?.status === 504
        ? `<b>服务端还没算完</b>(多半在等 AODP 限流),计算在后台继续,${craft.tries <= 2 ? "几秒后自动再要一次" : "过一会儿点「重算」"}。`
        : `<b>没算出来:</b>${esc(e?.message || e)}`;
    if (d && craft.sig === "") el.innerHTML += ` 下面还是上一次的结果。`;
    return;
  }
  if (!d) { el.className = "c-status"; el.innerHTML = ""; return; }
  const at = d.params_used?.now || d.books?.read_at;
  el.className = "c-status";
  const liveInfo = craft.keys.size
    ? (rt.up ? `实时:订了配方树上 ${num(craft.keys.size)} 个盘口,有成员翻到这些市场就自动重算`
      : `实时:推送断开了,重连后会自动补拉`) : "";
  el.innerHTML = `算于 ${hms(at)}${craft.liveNote ? ` · <b class="live">${esc(craft.liveNote)}</b>` : ""}${liveInfo ? ` · <span class="sub">${liveInfo}</span>` : ""}`;
}

// ── 结果 ──
function craftRender(res) {
  $("c-result").classList.remove("dim");
  $("c-result").innerHTML = craftHeadHTML(res) + craftCardHTML(res) + craftRawHTML(res) + craftCandsHTML(res) + craftDataHTML(res);
}

// ── 原料来路:直接回答「买 8.4 矿,还是 8.3/8.2/8.1/8.0,还是从低级转上来」(设计 §4.5) ──
// 按推荐那条路线的需求量,列每个起点「全部从这里买、再转化上来」的等效单价和可用件数,
// 再给混合最优的拆分(便宜的那几档直接买、其余从最便宜的起点转)。数来自 raw_sources
const RAW_ROWS = 8;   // 起点多(T8.4 矿有二十几个),默认列 8 行左右:直接买、同阶、低一阶必列(见 rawShown)
const levelOf = id => { const m = REFINE_ID_RE.exec(id); return m ? Number(m[1]) + Number(m[3] || 0) : -1; };
const pathText = path => (path || []).map(cTier).join(" → ");
const priceRange = s => (s.price_from === s.price_to || !s.price_to ? num(s.price_from) : `${num(s.price_from)} ~ ${num(s.price_to)}`);

function rawVerdict(id, src, res, main) {
  const T = cTier(id), need = src.need;
  const pure = src.pure || [], splits = src.mixed?.split || [];
  const direct = pure.find(p => p.start === id);
  const mixedU = src.mixed?.unit, directU = direct?.unit ?? null;
  const bestTrans = pure.find(p => p.start !== id && p.filled && p.unit != null);
  // 直接买凑不齐时 raw_sources 没有它的均价;卖一从「全部吃卖一」那条参照行的买料步骤里拿
  const mk = (res.candidates || []).find(c => c.reference === "market");
  const ask = mk?.steps?.find(s => s.op === "buy" && s.item === id && !s.for)?.price_from;
  const directWhy = direct ? (direct.filled ? "" : `直接买的盘口只有 ${fq(direct.available)} 个,不够 ${fq(need)} 个${
    ask ? `(卖一 ${num(ask)} 起${mixedU != null && ask > mixedU ? ",本身就比转化上来贵" : ""})` : ""}`) : `${T} 在可买城市没有卖单`;
  let v;
  if (!splits.length) {
    return `<b>缺价</b>:${esc(cLabel(id))} 和能转化到它的起点,在可买城市都没有挂单(或者超过了数据最大年龄)。`;
  }
  const fees = res.params_used?.fees || {};
  const at = s => (s.city && s.city !== res.city ? `在 ${esc(s.city)} ` : "");
  const split = s => s.bid ? `${at(s)}挂买单收 ${fq(s.qty)} 个(${priceRange(s)})`
    : s.start === id ? `${at(s)}直接买 ${fq(s.qty)} 个(${priceRange(s)})`
    : `${at(s)}从 ${esc(cTier(s.start))} 买 ${fq(s.qty)} 个(买价 ${priceRange(s)})转 ${Math.max(0, (s.path || []).length - 1)} 次上来`;
  // 拆分分三种来路:直接买、从别的起点转化、挂买单收。结论按实际有哪几种来写 ——
  // 以前只看「起点是不是它自己」,混合最优只有一段挂买单时落进「混着买最省」,
  // 说的「卖一附近直接买、再往上改从低级转」一样都没发生
  const directS = splits.filter(s => !s.bid && s.start === id);
  const transS = splits.filter(s => !s.bid && s.start !== id);
  const bidS = splits.filter(s => s.bid);
  // 读起来是「先买卖一附近那几件,其余从低级转,再挂买单收一些」
  const ordered = [...directS, ...transS, ...bidS];
  const both = directS.length && transS.length
    ? "卖一附近那几件便宜货直接买,再往上的挂单比转化贵,就改从低级转。" : "";
  let mentionsTrans = transS.length > 0;
  if (bidS.length) {
    const setup = fees.buy_order_setup === false ? 0 : fees.setup;
    v = `<b>${directS.length || transS.length ? "一部分挂买单收" : `挂买单收 ${esc(T)}`}</b>(纸面价):${ordered.map(split).join(" + ")};`
      + `纸面均价 ${num(mixedU)}(买一 + ${num(fees.outbid)} 银挂,含 ${pct(setup)} 创建费)。`
      + `<span class="neg">收不收得到、要等多久都不知道,挂上去没人卖也照扣创建费</span>`
      + (directU != null ? `;全部直接买(吃卖一)均价 ${num(directU)},这个纸面价${mixedU < directU ? `低 ${pct(1 - mixedU / directU)}` : `高 ${pct(mixedU / directU - 1)}`}。`
        : `;${directWhy}。`) + both;
  } else if (!transS.length) {
    // 只有直接买(可能分在几个城市买)
    v = `<b>直接买 ${esc(T)} 最划算</b>:${directS.length > 1 ? ordered.map(split).join(" + ") + ";" : ""}${fq(src.mixed?.got ?? need)} 个均价 ${num(mixedU)}。` + (bestTrans
      ? `转化上来最便宜的是从 ${esc(cTier(bestTrans.start))} 走(均价 ${num(bestTrans.unit)}),贵 ${pct(bestTrans.unit / mixedU - 1)}。`
      : "没有别的起点凑得齐这个量。");
    mentionsTrans = !!bestTrans;
  } else if (!directS.length) {
    v = `<b>别直接买 ${esc(T)}</b>:${ordered.map(split).join(" + ")},均价 ${num(mixedU)}` + (directU != null
      ? `,比全部直接买(均价 ${num(directU)})便宜 ${pct(1 - mixedU / directU)}。` : `;${directWhy}。`);
  } else {
    v = `<b>混着买最省</b>:${ordered.map(split).join(" + ")};均价 ${num(mixedU)}` + (directU != null
      ? `,比全部直接买(均价 ${num(directU)})每个省 ${num(directU - mixedU)}(${pct(1 - mixedU / directU)})。`
      : `;${directWhy}。`) + both;
  }
  if (src.mixed && !src.mixed.filled) v += ` <span class="neg">所有起点加起来也只凑到 ${fq(src.mixed.got)} 个,不够 ${fq(need)} 个。</span>`;
  // 只有 AODP 价的起点按不限量算(不知道挂了几件):AODP 的最低卖价可能只对应 1 件,结论得打个问号
  const unknown = cUniq(splits.filter(s => !s.bid && pure.find(p => p.start === s.start)?.available == null).map(s => s.start));
  if (unknown.length) {
    v += ` <span class="warnish"><b>${unknown.map(x => esc(cTier(x))).join("、")} 的价只有 AODP,不知道挂了几件</b>:按不限量算的,实际可能只有 1 件。`
      + `下单前进游戏看一眼,或者让成员去翻一下这个市场(翻过就有抓包件数)。</span>`;
  }
  // k 的提示只在结论里真的比了转化时给(直接买最划算却没有别的起点、纯挂买单,都和 k 无关)
  const k = res.params_used?.fees?.transmute_factor;
  const ku = (res.params_used?.unverified || []).find(u => u.key === "fees.transmute_factor");
  const kOpen = ku && !ku.confirmed;
  if (main && mentionsTrans) {
    v += ` <span class="sub">这个结论取决于转化系数 k(现在按 ${esc(k)}${kOpen ? (ku.is_default ? ",服务端默认、没核实" : ",服务端配置的、没核实") : ",你填的"}):k 一变,直接买和转化谁便宜可能反过来。</span>`
      + (kOpen ? ` <button class="act ghost mini-act" data-open-cal>去量 k</button>` : "");
  }
  return v;
}

// 原料来路表默认列哪些起点:用户问的就是「直接买 8.4,还是 8.3/8.2/8.1/8.0 升上来,还是 7.4 跨阶」,
// 所以直接买、同阶的每一档、低一阶同附魔那一档永远列出来(设计 §4.5);剩下的位置给最便宜的其它起点。
// 以前只按单价取前 8 个,8.x 同阶的几档常常比低 tier 的起点贵一点,全被折进「显示全部」
function rawShown(id, pure) {
  const m = REFINE_ID_RE.exec(id);
  if (!m) return pure.slice(0, RAW_ROWS);
  const t = Number(m[1]), e = Number(m[3] || 0), fam = m[2];
  const must = new Set([id, refineID(fam, t - 1, e)]);
  for (let x = 0; x < e; x++) must.add(refineID(fam, t, x));
  const extra = Math.max(2, RAW_ROWS - pure.filter(p => must.has(p.start)).length);
  let n = 0;
  return pure.filter(p => must.has(p.start) || n++ < extra);
}

function rawBlock(id, src, res, main) {
  const pure = src.pure || [], splits = src.mixed?.split || [];
  const direct = pure.find(p => p.start === id);
  const directU = direct?.unit ?? null;
  const all = craft.rawAll.has(id);
  const shown = all ? pure : rawShown(id, pure);
  const cheapest = pure.find(p => p.unit != null);
  const vs = u => (u == null || directU == null || !directU ? "—" : Math.abs(u / directU - 1) < 0.0005 ? "±0" : spct(u / directU - 1));
  const mixedDesc = splits.map(s => `${cityDot(s.city)} ${s.bid ? "挂买单收" : s.start === id ? "直接买" : `${esc(cTier(s.start))} 买来转`} <b>${fq(s.qty)}</b> 个 · ${priceRange(s)}${
    s.start === id || s.bid ? "" : ` · ${esc(pathText(s.path))}`}`).join("<br>");
  const rows = shown.map(p => {
    const isDirect = p.start === id, steps = Math.max(0, (p.path || []).length - 1);
    // 同阶:只升附魔(8.0→8.4);跨阶:从低一个 tier 上来(7.4→8.4、7.0→8.0→…)
    const cross = Number(REFINE_ID_RE.exec(p.start)?.[1]) !== Number(REFINE_ID_RE.exec(id)?.[1]);
    return `<tr class="${isDirect ? "direct" : ""}">
      <td class="l">${isDirect ? `<b>直接买 ${esc(cTier(id))}</b>` : `从 ${cItem(p.start, { noTag: true })} 转`}
        ${p === cheapest ? `<span class="wtag good">单一起点里最便宜</span>` : ""}</td>
      <td class="l sub">${isDirect ? "不转化" : `${esc(pathText(p.path))}(${steps} 次,${cross ? "跨阶" : "同阶升附魔"})`}</td>
      <td>${p.unit == null ? `<span class="neg">凑不齐</span>` : num(p.unit)}</td>
      <td class="${p.unit != null && directU != null && p.unit < directU ? "pos" : ""}">${isDirect ? "—" : vs(p.unit)}</td>
      <td title="${p.available == null ? "只有 AODP 价、不知道挂了多少件,按不限量算" : "可买城市里这个起点的可见件数合计"}">${p.available == null ? "深度未知" : num(p.available)}</td>
      <td>${p.available == null
        // 深度未知时服务端按不限量算,filled 一定是 true —— 不能跟着写「够」
        ? `<span class="warnish" title="只有 AODP 价、不知道挂了几件:按不限量算的,实际可能只有 1 件">不知道</span>`
        : p.filled ? "够" : `<span class="neg">不够</span>`}</td>
    </tr>`;
  }).join("");
  const more = pure.length > shown.length || all
    ? `<button class="act ghost mini-act" data-raw-all="${esc(id)}">${all ? "只看常用的几个起点" : `显示全部 ${pure.length} 个起点`}</button>` : "";
  return `<div class="c-rawblk${main ? " main" : ""}">
    <h4>${cItem(id)} <em>按推荐路线要 <b>${fq(src.need)}</b> 个</em></h4>
    <p class="c-verdict">${rawVerdict(id, src, res, main)}</p>
    <div class="tablewrap"><table class="tight c-rawt">
      <thead><tr><th class="l">起点</th><th class="l">怎么走</th><th title="每个 ${esc(cTier(id))} 的均价:买价沿阶梯吃深度 + 转化实付 + 运费">等效单价</th>
        <th title="和「全部直接买」比">比直接买</th><th>可用件数</th><th>够不够 ${fq(src.need)} 个</th></tr></thead>
      <tbody>
        <tr class="mixed"><td class="l"><b>混合最优</b> <span class="wtag good">推荐</span></td><td class="l">${mixedDesc || "—"}</td>
          <td><b>${src.mixed?.unit == null ? "—" : num(src.mixed.unit)}</b></td><td class="${src.mixed?.unit != null && directU != null && src.mixed.unit < directU ? "pos" : ""}">${vs(src.mixed?.unit)}</td>
          <td title="混合最优实际拿到的件数(不是盘口深度)">拿到 ${fq(src.mixed?.got)}</td>
          <td>${!src.mixed?.filled ? `<span class="neg">不够</span>`
            : splits.some(s => !s.bid && pure.find(p => p.start === s.start)?.available == null)
              ? `<span class="warnish" title="用到了只有 AODP 价的起点,按不限量算的">不知道</span>` : "够"}</td></tr>
        ${rows}
      </tbody></table></div>${more}
  </div>`;
}

function craftRawHTML(res) {
  const rs = res.raw_sources || {};
  const ids = Object.keys(rs);
  if (!ids.length) return "";
  // 和目标同 tier、同附魔的那种原料是主角(T8.4 条的 T8.4 矿),放最前;其余是下级链自炼时用到的
  const r = refineRole(res.target);
  const top = r && rs[r.rawID] ? r.rawID : ids.sort((a, b) => levelOf(b) - levelOf(a))[0];
  const others = ids.filter(x => x !== top).sort((a, b) => levelOf(b) - levelOf(a));
  const openOthers = craft.rawOpen;
  return `<section class="c-sec c-raw">
    <h3>原料来路 <span class="sub">买现成的 ${esc(cTier(top))},还是从低一级、低一阶转上来?按推荐路线的需求量,逐个起点算</span></h3>
    ${rawBlock(top, rs[top], res, true)}
    ${others.length ? `<details class="c-fold c-rawmore"${openOthers ? " open" : ""}><summary>下级链自炼用到的原料 <em>${others.map(cTier).join("、")}</em></summary>
      ${others.map(id => rawBlock(id, rs[id], res, false)).join("")}</details>` : ""}
  </section>`;
}

const BIND_WORD = { focus: "今日专注", volume: "成交量", capital: "本金", qty: "这一批的件数", take: "吃买单卖得掉的件数" };
const MODE_WORD = { post: "挂卖单", take: "吃买单" };
const UNVERIFIED_WORD = {
  "fees.transmute_factor": "转化系数 k", "focus.fce": "精炼专精 FCE", "focus.unlocked_tier": "精炼已解锁到",
  "fees.station_per100": "精炼站费", "fees.transmute_station_per100": "转化主人费率",
  "rrr.daily_bonus": "今日生产加成", heart_returns: "心也参与返还",
};
const cModes = res => res.params_used?.sell?.modes?.length ? res.params_used.sell.modes : ["post", "take"];
const cCapital = res => res.params_used?.daily?.capital ?? craft.sd?.capital ?? 0;

// 日化的几个上限,悬停看全(每天能做几件,取最小的那个;设计 §2.5)
function limitsText(d) {
  if (!d?.limits) return "";
  const L = d.limits, parts = [];
  const add = (k, v) => { if (v != null) parts.push(`${BIND_WORD[k]} ${fq(v)} 件/天`); };
  add("focus", L.focus); add("volume", L.volume); add("capital", L.capital); add("qty", L.qty); add("take", L.take);
  return `每天能做几件取这几个上限里最小的:\n${parts.join("\n")}\n现在卡在「${BIND_WORD[d.binding] || d.binding}」`;
}

// 每点专注值和 premium 门槛(D6):养一个 premium 小号换来的专注,每点值这么多银才回本
function spfBar(v) {
  const th = craftPremiumSPF();
  if (v == null || !isFinite(v)) return `<span class="sub">这条路线不用专注</span>`;
  const max = Math.max(th * 2, v * 1.15, 1);
  const w = Math.max(0, Math.min(100, v / max * 100)), m = th / max * 100;
  return `<div class="pfbar" title="每点专注值 ${num(v)} 银;premium 门槛 ${num(th)} 银/点(金价 ${num(craftGold())},3750 金 30 天、每天 10000 专注)">
      <i class="fill${v >= th ? " over" : ""}" style="width:${w.toFixed(1)}%"></i><i class="mark" style="left:${m.toFixed(1)}%"></i></div>
    <span class="sub">${v >= th ? "高于" : "低于"} premium 门槛 ${num(th)} 银/点${v >= th ? ":为专注开小号也划算" : ":只为这点专注开 premium 小号不划算"}</span>`;
}

// 推荐那条路线用得着哪些待核实参数:没有转化就不管 k 和主人费率,不开专注就不管专精,
// 没用心配方就不管心返还。以前 7 条全列,用不到的也挂着「没核实」,真该核的淹在里面
const UNV_APPLIES = {
  "fees.transmute_factor": c => c.steps?.some(s => s.op === "transmute"),
  "fees.transmute_station_per100": c => c.steps?.some(s => s.op === "transmute"),
  "focus.fce": c => c.focus?.tiers?.length > 0,
  "focus.unlocked_tier": () => true,   // 解锁到哪决定哪几级能自炼,永远用得着
  "fees.station_per100": c => c.steps?.some(s => s.op === "refine"),
  "rrr.daily_bonus": c => c.steps?.some(s => s.op === "refine"),
  heart_returns: c => c.steps?.some(s => s.op === "refine" && s.heart),
};
function unvValue(u) {
  return typeof u.value === "object" && u.value
    ? Object.entries(u.value).map(([t, f]) => `T${t} ${num(f)}`).join(" ")
    : typeof u.value === "boolean" ? (u.value ? "是" : "否")
    : u.key === "focus.unlocked_tier" ? `T${u.value}`
    : u.key === "rrr.daily_bonus" ? `+${u.value}`
    : /station_per100$/.test(u.key) ? `${num(u.value)} 银/100 营养` : String(u.value);
}
// 卡片上的「待核实参数」:用得着的逐项列(没核实的红、你填过的绿),用不着的一行带过,
// 怎么核对直接写出来(以前只在悬停提示里)
function craftUnvHTML(res, c) {
  const unv = res.params_used?.unverified || [];
  if (!unv.length) return "";
  const used = unv.filter(u => (UNV_APPLIES[u.key] || (() => true))(c));
  const unused = unv.filter(u => !used.includes(u));
  const open = used.filter(u => !u.confirmed);
  const items = used.map(u => {
    const when = cConfDate(u.key);
    const tag = u.confirmed ? `你填的${when ? " · " + when : ""}` : u.is_default ? "默认值,没核实" : "服务端配置的值,没核实";
    return `<li class="${u.confirmed ? "mine" : "def"}" title="${esc(`规则表 ${u.rule}。怎么核对:${u.check}`)}">
      <b>${esc(UNVERIFIED_WORD[u.key] || u.key)}</b> ${esc(unvValue(u))} <em>${esc(tag)}</em></li>`;
  }).join("");
  const how = open.length ? `<details class="c-howto"><summary>怎么核对这 ${open.length} 项</summary><ul>${open.map(u =>
    `<li><b>${esc(UNVERIFIED_WORD[u.key] || u.key)}</b>:${esc(u.check)} <span class="sub">(规则表 ${esc(u.rule)})</span></li>`).join("")}</ul>
    <p class="sub">核对完把数填进「高级参数」(填的正好是默认值也要填上),这里就变成「你填的」。</p></details>` : "";
  return `<div class="c-unv"><span>${open.length ? `这条路线用到 <b>${open.length} 项没核实的参数</b>:` : "这条路线用到的待核实参数<b>都是你填的</b>:"}</span>
      <ul>${items}</ul>${open.length ? `<button class="act ghost mini-act" data-open-cal>去校准</button>` : ""}</div>
    ${unused.length ? `<p class="c-unv-skip sub">这条路线用不到:${unused.map(u => esc(UNVERIFIED_WORD[u.key] || u.key)).join("、")}</p>` : ""}
    ${how}`;
}

function craftCardHTML(res) {
  const c = (res.candidates || []).find(x => !x.reference);
  if (!c) {
    const why = (res.warnings || []).find(w => w.code === "locked");
    return `<div class="c-card empty"><b>${why ? esc(why.msg) : "没有算出候选路线"}</b>
      ${why ? `<p class="sub">在「高级参数」里改「精炼已解锁到」,或者换一个已解锁的 tier。</p>` : ""}</div>`;
  }
  const modes = cModes(res), m0 = modes[0], m1 = modes.find(m => m !== m0);
  const s = res.sell || {}, p0 = c.profit?.[m0], p1 = m1 ? c.profit?.[m1] : null;
  const cap = cCapital(res), over = cap > 0 && c.upfront_capital > cap;
  // 税、创建费、压价都从这次实际用的参数取(服务端 economics 配置,税率可能被装备减过),不写死
  const fees = res.params_used?.fees || {};
  const tax = fees.tax ?? 0, setup = fees.setup ?? 0;
  const net = m0 === "take"
    ? (s.take ? `<b>${num(s.take.fill?.net_avg)}</b><i>吃买单均价 ${num(s.take.fill?.vwap)} × ${ratio(1 - tax)}(扣 ${ppct(tax)} 税)</i>
        <em>这一批吃得掉 ${fq(s.take.fill?.got)} 件${s.take.max_qty != null ? `(断崖前最多 ${fq(s.take.max_qty)} 件)` : "(只有 AODP 价,按全卖得掉算)"}</em>`
      : `<b>—</b><i>没有买单</i>`)
    : (s.post ? `<b>${num(s.post.net)}</b><i>挂 ${num(s.post.price)}(卖一 ${num(s.post.best_ask)} 压 ${num(fees.undercut)} 银)× ${ratio(1 - tax - setup)}</i>
        <em>扣 ${ppct(tax)} 税 + ${ppct(setup)} 创建费</em>` : `<b>—</b><i>没有卖单</i>`);
  const profitLine = (pl, mode) => !pl ? `${MODE_WORD[mode]} —`
    : `${MODE_WORD[mode]} <span class="${pl.unit >= 0 ? "pos" : "neg"}">${sgn(pl.unit)}(${spct(pl.pct)})</span>${
      pl.sold != null && pl.sold < res.qty - 0.005 ? `,只出得掉 ${fq(pl.sold)} 件` : ""}`;
  const d = c.daily;
  const unvHTML = craftUnvHTML(res, c);
  const f = c.focus || {};
  // 「推荐」不是无条件的:拿不满料、或者按所选卖法所有路线都亏,卡片头就直说,
  // 不能让扫一眼的人把一条亏钱 / 凑不齐料的路线当成推荐
  const normal = (res.candidates || []).filter(x => !x.reference && x.filled);
  const anyPos = normal.some(x => (x.profit?.[m0]?.unit ?? -1) >= 0);
  const altBest = m1 ? Math.max(-Infinity, ...normal.map(x => x.profit?.[m1]?.unit).filter(v => v != null)) : -Infinity;
  const losing = c.filled && p0 && !anyPos;
  const kicker = !c.filled ? `<span class="c-kicker bad">拿不满料</span>`
    : losing ? `<span class="c-kicker bad">最好的一条也亏</span>` : `<span class="c-kicker">推荐</span>`;
  const loseLn = losing ? `<p class="c-unfilled"><b>按${MODE_WORD[m0]}卖,所有路线都亏。</b>${altBest > 0
    ? `改${MODE_WORD[m1]}的话最好的一条每根 ${sgn(altBest)}:在上面「出售方式」切过去看。`
    : `${m1 ? `${MODE_WORD[m1]}也亏。` : ""}别做;或者换生产 / 出售城市、等价格回来再算。`}</p>` : "";
  const unfilledLn = c.filled ? "" : `<p class="c-unfilled"><b>拿不满料:</b>要的原料或下级料缺价、盘口不够深,下面的成本只按拿得到的那部分算,是下界,利润不能当真。${
    res.data?.missing?.length ? `缺价的:${res.data.missing.map(x => esc(cLabel(x) || x)).join("、")}。` : ""}换个城市、打开「跨城买料」,或者让成员去游戏里翻一下这些市场。</p>`;
  // 本金撑得起一批几件:日化的本金上限 × 周转天数 =(本金 − 批末余料)÷ 稳态单根成本。
  // 默认 10 件对高 tier 常常备不齐,光说「超过几倍」不够,给个能直接点的件数
  const lc = d?.limits?.capital, turn = res.params_used?.daily?.turnover_days || 1;
  const fit = c.filled && lc != null ? Math.floor(lc * turn + 1e-9) : null;
  const fitLn = fit == null ? "" : fit < 1
    ? `<br><b class="neg">本金连 1 件都备不齐</b>(批末余料就要占一块)`
    : `<br>本金最多撑 ≈${num(fit)} 件/批${over && fit < res.qty ? ` <button class="act ghost mini-act" data-set-qty="${fit}">按本金设成 ${num(fit)} 件</button>` : ""}`;
  const dimmed = !c.filled;
  return `<section class="c-card${dimmed ? " unfilled" : losing ? " losing" : ""}">
    <div class="c-card-head">
      <div>${kicker} <b class="c-route">${esc(c.label)}</b></div>
      <button class="act ghost" data-open-steps="${esc(c.id)}">看步骤清单 ↓</button>
    </div>
    ${unfilledLn}${loseLn}
    ${craftBuyLine(res)}
    <div class="kpis c-kpis${dimmed ? " dimmed" : ""}">
      <div><b>${dimmed ? "≥ " : ""}${num(c.unit_cost)}</b><i>单根成本</i><em>${dimmed ? "只按拿得到的料算,是下界" : `这一批 ${num(res.qty)} 件共 ${num(c.total_cost)}`}</em></div>
      <div>${net}</div>
      ${dimmed ? `<div><b>—</b><i>单根利润(${MODE_WORD[m0]})</i><em>拿不满料,算不准</em></div>`
        : `<div><b class="${p0 && p0.unit >= 0 ? "pos" : "neg"}">${p0 ? sgn(p0.unit) : "—"}</b><i>单根利润(${MODE_WORD[m0]})· 利润率 ${p0 ? spct(p0.pct) : "—"}</i>
        <em>这一批 ${p0 ? sgn(p0.total) : "—"} · ${m1 ? profitLine(p1, m1) : ""}</em></div>`}
      <div><b>${dimmed || c.silver_per_focus == null ? "—" : snum(c.silver_per_focus)}</b><i>每点专注值(银)</i>${dimmed ? `<em>拿不满料,算不准</em>` : spfBar(c.silver_per_focus)}</div>
      <div title="${esc(limitsText(d))}"><b class="${dimmed ? "" : d && d.profit >= 0 ? "pos" : "neg"}">${d && !dimmed ? sgn(d.profit) : "—"}</b><i>日化收益 / 天</i>
        <em>${dimmed ? "拿不满料,算不准" : d ? `每天 ${fq(d.units)} 件 · 卡在<b>${esc(BIND_WORD[d.binding] || d.binding)}</b>` : "算不出(没有卖价)"}</em></div>
      <div class="${over && !dimmed ? "c-over" : ""}"><b>${dimmed ? "≥ " : ""}${num(c.upfront_capital)}</b><i>开工本金(含批末余料)</i>
        <em>${dimmed ? "只按拿得到的料算,是下界" : cap > 0 ? (over ? `<b class="neg">超过本金 ${num(cap)}(${(c.upfront_capital / cap).toFixed(1)} 倍)</b>:一次备不齐,把件数调小或分批` : `本金 ${num(cap)} 以内`) : "不按本金封顶"}${dimmed ? "" : fitLn}</em></div>
      <div><b>${ageTag(c.oldest_age_h)}</b><i>用到的价里最旧的</i><em>${c.oldest_age_h == null ? "没有用到带观测时间的价"
        : c.age_level === "red" ? "超过 24 小时,先进游戏看一眼" : c.age_level === "yellow" ? "超过 6 小时,下单前核一下" : "6 小时以内"}</em></div>
    </div>
    ${craftFocusLine(res, c)}
    ${craftFocusHTML(res, c)}
    ${unvHTML}
    ${warnList(c.warnings)}
  </section>`;
}
const ratio = x => (x == null || !isFinite(x) ? "—" : String(Math.round(x * 1000) / 1000));
const ppct = x => (x == null || !isFinite(x) ? "—" : `${Math.round(x * 10000) / 100}%`);
const tiersText = ts => (ts?.length ? "T" + [...ts].sort((a, b) => b - a).join("+T") : "不开专注");

// 卡片里一句话的「买料:」:原料来路那张表的结论在一屏以下,这里先给出怎么买
function craftBuyLine(res) {
  const r = refineRole(res.target), id = r?.rawID, src = id && res.raw_sources?.[id];
  if (!src) return "";
  const splits = src.mixed?.split || [], pure = src.pure || [];
  const at = s => (s.city && s.city !== res.city ? `在 ${esc(s.city)} ` : "");
  const parts = [...splits.filter(s => !s.bid && s.start === id), ...splits.filter(s => !s.bid && s.start !== id), ...splits.filter(s => s.bid)]
    .map(s => s.bid ? `${at(s)}挂买单收 ${fq(s.qty)} 个` : s.start === id ? `${at(s)}直接买 ${fq(s.qty)} 个`
      : `${at(s)}从 ${esc(cTier(s.start))} 买 ${fq(s.qty)} 个转上来`);
  const unknown = splits.some(s => !s.bid && pure.find(p => p.start === s.start)?.available == null);
  const text = !parts.length ? `<span class="neg">${esc(cLabel(id))} 缺价</span>`
    : `${esc(cLabel(id))} 要 ${fq(src.need)} 个:${parts.join(" + ")},均价 ${num(src.mixed?.unit)}${
      src.mixed?.filled ? "" : `,<span class="neg">凑不齐</span>`}${unknown ? `;<span class="warnish">有只有 AODP 价的,件数不知道</span>` : ""}`;
  return `<p class="c-buyln"><b>买料:</b>${text} <button class="act ghost mini-act" data-goto-raw>看原料来路 ↓</button></p>`;
}

// 专注这一行:开了哪几级、这一批要多少;超过今日可用时说清楚要攒几天,日化已经按每天的专注封顶
function craftFocusLine(res, c) {
  const f = c.focus || {}, budget = res.focus?.budget;
  const perDay = craft.sd?.focus_per_day || 10000;
  let over = "";
  if (f.over_budget) {
    over = ` <span class="wtag bad">这一批要的专注超过今日可用</span> 按每天恢复 ${num(perDay)} 算,这一批要攒约 ${
      (f.effective / perDay).toFixed(1)} 天;日化已经按每天的专注封顶算过(见「日化」格悬停)。`;
  }
  const pick = res.focus?.budget_pick;
  const pickLn = pick && !sameTiers(pick.tiers, f.tiers)
    ? ` · 今日专注以内成本最低的开法是「${esc(tiersText(pick.tiers))}」(单根成本 ${num(pick.unit_cost)},见下面「专注开法」)` : "";
  return `<p class="c-focusln">专注:${f.tiers?.length ? `${esc(tiersText(f.tiers))} 开,每根 ${num(f.per_unit)}(没有专精时 ${num(f.base_per_unit)})· 这一批 ${num(f.effective)}` : "不开"}
    · 今日可用 ${num(budget)}${over}${pickLn}</p>`;
}
const sameTiers = (a, b) => (a || []).length === (b || []).length && (a || []).every(t => (b || []).includes(t));

// 「专注开法」:服务端枚举了链上每一级专注开 / 关,留下「多花专注、成本更低」的 Pareto 前沿
// (focus.pareto,都是「原料混合最优 · 下级料自动比价」)。回答「值不值得花专注、花在哪几级」:
// 不开专注是亏是赚、每多开一级多赚多少、今日专注够不够。以前这些字段一个都没显示
function craftFocusHTML(res, c) {
  const pts = res.focus?.pareto || [];
  if (!pts.length) return "";
  const m0 = cModes(res)[0], s = res.sell || {};
  const net = m0 === "take" ? (s.take?.fill?.got > 0 ? s.take.fill.net_avg : null) : s.post?.net ?? null;
  const none = pts.find(p => !p.tiers?.length);
  const base = none && net != null ? Math.max(0, net - none.unit_cost) : null;
  const perDay = craft.sd?.focus_per_day || 10000, th = craftPremiumSPF();
  const pick = res.focus?.budget_pick;
  const rows = pts.map((p, i) => {
    const profit = net != null ? net - p.unit_cost : null;
    const spf = p.focus_per_unit > 0 && profit != null && base != null ? (profit - base) / p.focus_per_unit : null;
    const prev = pts[i - 1];
    const marg = prev && p.focus_per_unit > prev.focus_per_unit ? (prev.unit_cost - p.unit_cost) / (p.focus_per_unit - prev.focus_per_unit) : null;
    const isPick = pick && sameTiers(pick.tiers, p.tiers), isRec = sameTiers(c.focus?.tiers, p.tiers);
    return `<tr class="${isPick ? "pick" : ""}">
      <td class="l">${esc(tiersText(p.tiers))}${isPick ? ` <span class="wtag good" title="今日可用专注以内,单根成本最低">今日预算内最省</span>` : ""}${isRec ? ` <span class="wtag">推荐那条用的</span>` : ""}</td>
      <td>${num(p.focus_per_unit)}</td><td>${num(p.unit_cost)}</td>
      <td class="${profit == null ? "" : profit >= 0 ? "pos" : "neg"}">${sgn(profit)}</td>
      <td class="${spf == null ? "" : spf >= th ? "pos" : ""}">${spf == null ? "—" : snum(spf)}${marg != null ? `<span class="subln">比上一行多花的每点 ${snum(marg)}</span>` : ""}</td>
      <td>${num(p.focus_total)}</td>
      <td>${p.within_budget ? "够" : `<span class="warnish">约 ${(p.focus_total / perDay).toFixed(1)} 天的量</span>`}</td></tr>`;
  }).join("");
  const sum = none && net != null
    ? `不开专注每根 ${sgn(net - none.unit_cost)}${pick ? ` · 今日预算内最好:${esc(tiersText(pick.tiers))}(每根 ${sgn(net - pick.unit_cost)})` : ""}`
    : "没有卖价或不开专注凑不齐料,只能比成本";
  return `<details class="c-focusfold"${craft.focusOpen ? " open" : ""}><summary>专注开法对比(${pts.length} 种)<em>${sum}</em></summary>
    <div class="tablewrap"><table class="tight c-focust">
      <thead><tr><th class="l">开专注的级</th><th>每根专注</th><th>单根成本</th><th>单根利润(${MODE_WORD[m0]})</th>
        <th title="(这种开法的利润 − max(0, 不开专注的利润))÷ 每根专注;绿色 = 超过 premium 门槛 ${num(th)}">每点专注值</th>
        <th>这一批专注</th><th title="今日可用 ${num(res.focus?.budget)};不够的按每天恢复 ${num(perDay)} 折成天数">今日够不够</th></tr></thead>
      <tbody>${rows}</tbody></table></div>
    <p class="sub">每一行都按「原料混合最优 · 下级料自动比价」算,只换专注开在哪几级;只列「多花专注、成本确实更低」的开法。
      专注按次扣、和返还无关,不开专注的那几级回收率低、要多买料。</p>
  </details>`;
}

// ── 候选路线表(前 N 条 + 两条参照行) ──
const REF_NOTE = {
  market: "参照:全部吃卖一、不转化、下级料买现成 —— 什么都不优化时的成本,拿来比",
  bid: "参照:原料挂买单收 —— 纸面上便宜,但收不收得到、要等多久都不知道,创建费下单就扣",
};
function craftCandsHTML(res) {
  const cands = res.candidates || [];
  if (!cands.length) return "";
  const cap = cCapital(res), th = craftPremiumSPF();
  const modes = cModes(res);
  const row = (c, i) => {
    const pp = c.profit?.post, pt = c.profit?.take, d = c.daily, f = c.focus || {};
    const open = craft.open.has(c.id);
    // 拿不满料:成本只是拿到的那一部分除以整批件数,是下界;照常按正数、绿色显示的话,
    // 「全部吃卖一」这种参照行看起来比推荐那条便宜得多、利润高得多 —— 正好反着读
    const lb = !c.filled;
    const LB_TIP = "拿不满料:成本只按拿得到的那部分算,是下界,利润、每点专注值、日化都不能当真";
    const profitCell = (pl, mode) => !pl ? `<td class="sub">—</td>`
      : lb ? `<td class="sub${mode === modes[0] ? " primary" : ""}" title="${LB_TIP}">—</td>`
      : `<td class="${pl.unit >= 0 ? "pos" : "neg"}${mode === modes[0] ? " primary" : ""}">${sgn(pl.unit)}<span class="subln">${spct(pl.pct)}${
        pl.sold != null && pl.sold < res.qty - 0.005 ? ` · 出 ${fq(pl.sold)} 件` : ""}</span></td>`;
    const codes = new Map();
    for (const w of c.warnings || []) if (!codes.has(w.code)) codes.set(w.code, w);
    return `<tr class="main${c.reference ? " refrow" : ""}${c.filled ? "" : " unfilled"}${open ? " open" : ""}" data-cid="${esc(c.id)}">
      <td class="l">${c.reference ? `<span class="kind flip">参照</span> ` : `<span class="rank">${i + 1}</span> `}<span class="c-route">${esc(c.label)}</span>
        ${c.reference ? `<span class="subln">${esc(REF_NOTE[c.reference] || "参照行")}</span>` : ""}</td>
      <td${lb ? ` title="${LB_TIP}"` : ""}>${lb ? `≥ ${num(c.unit_cost)}<span class="subln">只拿到一部分料</span>` : num(c.unit_cost)}</td>
      ${profitCell(pp, "post")}${profitCell(pt, "take")}
      <td class="${f.over_budget ? "neg" : ""}" title="${esc(f.tiers?.length ? `开专注的级:T${f.tiers.join("、T")};没有专精时每根 ${num(f.base_per_unit)}${f.over_budget ? ";这一批超过今日可用专注" : ""}` : "不开专注")}">${f.per_unit ? num(f.per_unit) : "0"}</td>
      ${lb ? `<td class="sub" title="${LB_TIP}">—</td>`
        : `<td class="${c.silver_per_focus != null && c.silver_per_focus >= th ? "pos" : c.silver_per_focus < 0 ? "neg" : ""}">${c.silver_per_focus == null ? "—" : snum(c.silver_per_focus)}</td>`}
      ${lb ? `<td class="sub" title="${LB_TIP}">—</td>`
        : `<td title="${esc(limitsText(d))}">${d ? sgn(d.profit) : "—"}<span class="subln">${d ? `${fq(d.units)} 件/天 · ${esc(BIND_WORD[d.binding] || d.binding)}` : ""}</span></td>`}
      <td class="${!lb && cap > 0 && c.upfront_capital > cap ? "neg" : ""}">${lb ? "≥ " : ""}${silver(c.upfront_capital)}</td>
      <td>${ageTag(c.oldest_age_h)}</td>
      <td class="l wcell">${c.filled ? "" : `<span class="wtag bad">拿不满料</span>`}${[...codes.values()].map(warnTag).join("")}</td>
    </tr>${open ? `<tr class="detail"><td colspan="10">${craftStepsHTML(c, res)}</td></tr>` : ""}`;
  };
  const normal = cands.filter(c => !c.reference), refs = cands.filter(c => c.reference);
  return `<section class="c-sec">
    <h3>候选路线 <span class="sub">前 ${normal.length} 条,按${esc({ daily: "日化收益", unit: "单根利润", per_focus: "每点专注值", pct: "利润率" }[res.params_used?.sort] || "日化收益")}排;
      点一行看步骤清单。利润主列是${MODE_WORD[modes[0]]}</span></h3>
    <div class="tablewrap"><table class="tight c-cands">
      <thead><tr><th class="l">路线</th><th title="每件的稳态成本(买料 + 转化 + 站费,批末余料按所选方式估值)">单根成本</th>
        <th title="挂卖单:卖一压 1 银挂上去,到手 ×(1 − 税 − 创建费)">挂卖利</th>
        <th title="吃买单:沿买单阶梯卖,到手 ×(1 − 税),断崖之后不卖">吃买单利</th>
        <th title="按你的专精 FCE 折算后,每件多少专注">专注/根</th>
        <th title="(开专注的利润 − max(0, 不开专注的最好利润))÷ 每件专注:不开专注本来就亏的话,按什么都不做(0)当基准。绿色 = 超过 premium 门槛">银/专注点</th>
        <th title="每天件数(专注、成交量、本金、批量里最小的)× 单根利润">日化</th>
        <th title="开工前要备齐的钱:稳态消耗 + 批末余料">本金</th>
        <th title="用到的价里最旧的:超过 6 小时黄、超过 24 小时红">数据</th><th class="l">警告</th></tr></thead>
      <tbody>${normal.map(row).join("")}
        ${refs.length ? `<tr class="sep"><td colspan="10">参照行(不参与推荐)</td></tr>${refs.map(row).join("")}` : ""}</tbody>
    </table></div>
  </section>`;
}

// 步骤清单:买什么、在哪城、多少件、价位、金额、观测时间和来源;转化几次、从哪到哪;
// 精炼几次、开没开专注、回收率;怎么卖;批末余料(设计 §4.5)
// 转化步骤按「最后变成哪种原料」分组:每组从买来的起点沿用到的边一路走到那种原料,组内按路径先后排,
// 带小计。服务端按起点的 tier+附魔 排,几条链的步骤交错在一起,照着在游戏里做很难跟。
// 两条链共用的一步(次数是合计)放在先出现的那组,后面的组里注明「也供」
function transGroups(buys, trans) {
  const adj = new Map();
  for (const t of trans) {
    if (!adj.has(t.from)) adj.set(t.from, []);
    adj.get(t.from).push(t);
  }
  // 从 S 走到 T 用到的边(广度优先;每一步都是 tier+附魔 加一,路径很短)
  const path = (S, T) => {
    const prev = new Map([[S, null]]), queue = [S];
    while (queue.length) {
      const v = queue.shift();
      if (v === T) break;
      for (const e of adj.get(v) || []) if (!prev.has(e.to)) { prev.set(e.to, e); queue.push(e.to); }
    }
    if (!prev.has(T)) return [];
    const out = [];
    for (let e = prev.get(T); e; e = prev.get(e.from)) out.unshift(e);
    return out;
  };
  const groups = [], rowOf = new Map();
  const targets = cUniq(buys.filter(b => b.for && b.for !== b.item).map(b => b.for)).sort((a, b) => levelOf(b) - levelOf(a) || (a < b ? -1 : 1));
  for (const T of targets) {
    const starts = cUniq(buys.filter(b => b.for === T).map(b => b.item));
    const g = { target: T, starts, rows: [] };
    const edges = cUniq(starts.flatMap(S => path(S, T))).sort((a, b) => levelOf(a.from) - levelOf(b.from) || (a.from < b.from ? -1 : 1));
    for (const e of edges) {
      const r = rowOf.get(e);
      if (r) { if (!r.also.includes(T)) r.also.push(T); continue; }
      const row = { t: e, also: [] };
      rowOf.set(e, row);
      g.rows.push(row);
    }
    if (g.rows.length) groups.push(g);
  }
  const rest = trans.filter(t => !rowOf.has(t)).map(t => ({ t, also: [] }));
  if (rest.length) groups.push({ target: "", starts: [], rows: rest });
  return groups;
}

// 拿不满料时每种料差多少:要的 = 各级精炼的稳态投入 − 下一级精炼做出来的;拿到的 = 买来(或买来转化成它)的件数
function shortfalls(buys, refs) {
  const need = new Map(), got = new Map();
  const add = (m, k, q) => m.set(k, (m.get(k) || 0) + q);
  for (const r of refs) for (const i of r.inputs || []) add(need, i.item, i.qty);
  for (const r of refs) add(need, r.item, -r.qty);
  for (const b of buys) add(got, b.for || b.item, b.qty);
  const out = new Map();
  for (const [item, n] of need) {
    const g = got.get(item) || 0;
    if (n > 0.01 && g < n - 0.01) out.set(item, { need: n, got: g });
  }
  return out;
}

function craftStepsHTML(c, res) {
  const steps = c.steps || [];
  const buys = steps.filter(s => s.op === "buy" || s.op === "bid");
  const trans = steps.filter(s => s.op === "transmute");
  const refs = steps.filter(s => s.op === "refine");
  const sell = steps.find(s => s.op === "sell");
  const sum = xs => xs.reduce((a, s) => a + (s.cost || 0), 0);
  const fees = res.params_used?.fees || {};
  const short = c.filled ? new Map() : shortfalls(buys, refs);
  const out = [];
  if (short.size) {
    out.push(`<p class="c-unfilled"><b>拿不满料:</b>${[...short].map(([item, x]) =>
      `${cItem(item)} 要 ${fq(x.need)} 个,${x.got > 0 ? `只买到 ${fq(x.got)} 个` : "一个都没买到"}`).join(";")}。
      下面的件数、金额都只是拿到的那一部分,精炼那一步照整批写,凑不齐是做不出来的。</p>`);
  }
  if (buys.length) {
    out.push(`<h4>① 买料 <em>${buys.length} 项,合计 ${num(sum(buys))} 银</em></h4>
      <table class="tight"><thead><tr><th class="l">物品</th><th class="l">城市</th><th>件数</th><th>价位</th><th>金额</th>
        <th class="l">观测</th><th class="l">用途</th></tr></thead><tbody>${buys.map(s => {
      const lv = s.levels || [];
      const price = s.price_from === s.price_to || !s.price_to ? num(s.price_from) : `${num(s.price_from)} ~ ${num(s.price_to)}`;
      const lvTitle = lv.map(l => `${num(l.price)} × ${fq(l.qty)}`).join("\n");
      const sh = !s.for ? short.get(s.item) : null;
      return `<tr${sh ? ` class="short"` : ""}><td class="l">${s.op === "bid" ? `<span class="wtag" title="挂买单:按买一 + ${num(fees.outbid)} 挂、多付创建费,收不收得到不知道">挂买单</span>` : ""}${cItem(s.item)}</td>
        <td class="l">${cityDot(s.city)}</td><td>${fq(s.qty)}${sh ? `<span class="subln neg">要 ${fq(sh.need)}</span>` : ""}</td>
        <td title="${esc(lvTitle)}">${price}${lv.length > 1 ? `<span class="subln">吃 ${lv.length} 档</span>` : ""}</td>
        <td>${num(s.cost)}${s.transport ? `<span class="subln">含运费 ${num(s.transport)}</span>` : ""}</td>
        <td class="l">${ageTag(s.age_h)} <span class="sub">${esc(SRC_NAME[s.source] || s.source || "")}${s.seen_at ? " · " + hms(s.seen_at) : ""}</span></td>
        <td class="l">${s.for ? `转化成 ${cItem(s.for, { noTag: true })}` : ""}</td></tr>`;
    }).join("")}</tbody></table>`);
  }
  if (trans.length) {
    const groups = transGroups(buys, trans);
    out.push(`<h4>② 转化 <em>${trans.length} 步,合计 ${num(sum(trans))} 银 · 在 ${esc(res.city)} 的站里做,不用专注、没有返还</em></h4>
      <table class="tight c-transt"><thead><tr><th class="l">从 → 到</th><th>次数</th><th>每次实付</th><th>合计</th></tr></thead><tbody>${groups.map(g =>
      `<tr class="grp"><td class="l" colspan="3">${g.target ? `做成 ${cItem(g.target)} <span class="sub">从 ${g.starts.map(x => esc(cTier(x))).join("、")} 一路转上去</span>` : "其它"}</td>
        <td>小计 ${num(g.rows.reduce((a, r) => a + (r.t.cost || 0), 0))}</td></tr>${g.rows.map(({ t, also }) =>
      `<tr><td class="l">${cItem(t.from, { noTag: true })} → ${cItem(t.to, { noTag: true })}${also.length
        ? `<span class="subln">这一步也供 ${also.map(x => esc(cTier(x))).join("、")}(次数是合计)</span>` : ""}</td><td>${fq(t.qty)}</td>
        <td>${num(t.unit_fee)}</td><td>${num(t.cost)}</td></tr>`).join("")}`).join("")}</tbody></table>`);
  }
  if (refs.length) {
    out.push(`<h4>${trans.length ? "③" : "②"} 精炼 <em>站费合计 ${num(sum(refs))} 银 · 专注合计 ${num(refs.reduce((a, s) => a + (s.focus_cost || 0), 0))}</em></h4>
      <table class="tight"><thead><tr><th class="l">产物</th><th>次数</th><th class="l">专注</th><th>回收率</th><th>站费</th>
        <th>专注消耗</th><th class="l">每批稳态投入</th></tr></thead><tbody>${refs.map(s =>
      `<tr><td class="l">${cItem(s.item)}${s.heart ? ` <span class="rtag heart" title="用心的那条配方">心配方</span>` : ""}</td><td>${fq(s.qty)}</td>
        <td class="l">${s.focus_on ? `<b>开</b>` : "不开"}</td><td>${pct(s.rrr)}</td><td>${num(s.station_fee)}</td>
        <td title="基础 ${num(s.focus_base)}">${s.focus_on ? num(s.focus_cost) : "—"}</td>
        <td class="l">${(s.inputs || []).map(i => `${fq(i.qty)} × ${cItem(i.item, { noTag: true })}`).join(" + ")}</td></tr>`).join("")}</tbody></table>`);
  }
  if (sell) {
    const unsold = sell.unsold ? `;<span class="neg">还有 ${fq(sell.unsold)} 件卖不掉</span>(断崖或买单见底),按成本留在手里,不算进利润` : "";
    out.push(`<h4>出售</h4><p class="c-p">${sell.mode === "take" ? "吃买单" : "挂卖单"} ${fq(sell.qty)} 件 @ ${cityDot(sell.city)},${
      sell.mode === "take" ? "均价" : "挂"} ${num(sell.price)},每件到手 ${num(sell.net)}${unsold} · ${ageTag(sell.age_h)} <span class="sub">${esc(SRC_NAME[sell.source] || sell.source || "")}</span></p>`);
  }
  const lvWord = { carry: "结转下一批", bid: "砸买单", zero: "按 0" };
  const left = c.leftover || [];
  const acq = left.reduce((a, l) => a + (l.cost || 0), 0), loss = left.reduce((a, l) => a + (l.loss || 0), 0);
  if (left.length) {
    out.push(`<h4>批末余料 <em>一批做完总会剩这些,和件数无关;取得成本合计 ${num(acq)}${Math.abs(loss) > 0.5 ? ` · 估值损失合计 ${snum(loss)}` : ""}</em></h4>
      <table class="tight"><thead><tr><th class="l">物品</th><th>件数</th><th>取得成本</th><th>估值</th><th class="l">方式</th>
        <th title="取得成本 − 估值;负数 = 估值比取得成本高">损失</th></tr></thead><tbody>${
      left.map(l => `<tr><td class="l">${cItem(l.item)}</td><td>${fq(l.qty)}</td><td>${num(l.cost)}</td><td>${num(l.value)}</td>
        <td class="l">${esc(lvWord[l.mode] || l.mode)}</td><td class="${l.loss > 0.5 ? "neg" : l.loss < -0.5 ? "pos" : ""}">${snum(l.loss)}</td></tr>`).join("")}</tbody></table>`);
  }
  // 对账:①+②+③ 是稳态;余料按砸买单 / 按 0 估值时的损失另外摊进成本 —— 以前只列了取得成本,
  // 照着加出来和卡片上的「这一批共 X」差一截,找不到差在哪
  const steady = sum(buys) + sum(trans) + sum(refs);
  out.push(`<p class="c-recon"><b>对账:</b>${trans.length ? "① 买料 + ② 转化 + ③ 站费" : "① 买料 + ② 站费"} = 稳态 ${num(steady)}${
    Math.abs(loss) > 0.5 ? ` ${loss > 0 ? "+" : "−"} 余料估值损失 ${num(Math.abs(loss))}(${esc(lvWord[left[0]?.mode] || "")})` : ""}
    = 这一批 ${num(c.total_cost)}(单根 ${num(c.unit_cost)} × ${num(res.qty)});开工本金 = 稳态 ${num(steady)}${
    left.length ? ` + 余料取得成本 ${num(acq)}` : ""} = ${num(c.upfront_capital)}${
    left.length && left.every(l => l.mode === "carry") ? "。余料按成本结转下一批,不算损失" : ""}</p>`);
  out.push(warnList(c.warnings));
  return `<div class="detailbox c-steps">${out.join("")}
    <p class="sub">件数是稳态均摊:按回收率的期望返还算,所以带小数;进游戏按整数下单,多出来的零头就是下一批的余料。</p></div>`;
}

// toggle 不冒泡,在捕获阶段听:记下「下级链的原料」开没开,重画后照旧
$("c-result").addEventListener("toggle", e => {
  if (e.target.classList?.contains("c-rawmore")) craft.rawOpen = e.target.open;
  // 默认折着:带 open 建出来触发的 toggle 记下的也是 true,和用户的选择一致
  if (e.target.classList?.contains("c-focusfold")) craft.focusOpen = e.target.open;
  // 「数据」那一块:重画时带着 open 属性建出来也会触发 toggle,只记用户点 summary 的那一次
  if (e.target.classList?.contains("c-data") && craft.dataToggleArmed) craft.dataOpen = e.target.open;
}, true);
// toggle 事件分不出是用户点的还是重画时带着 open 属性建出来的:只在点 summary 之后认一次
$("c-result").addEventListener("click", e => {
  if (e.target.closest(".c-data > summary")) {
    craft.dataToggleArmed = true;
    setTimeout(() => { craft.dataToggleArmed = false; }, 100);
  }
}, true);
$("c-result").addEventListener("click", e => {
  const openCal = e.target.closest("[data-open-cal]");
  if (openCal) {
    $("c-cal").open = true;
    $("c-cal").scrollIntoView({ block: "start", behavior: "smooth" });
    return;
  }
  const sq = e.target.closest("[data-set-qty]");
  if (sq) {
    const n = Number(sq.dataset.setQty);
    if (Number.isInteger(n) && n >= 1 && n <= 10000) { craft.p.qty = n; craftChanged({ now: true }); }
    return;
  }
  if (e.target.closest("[data-goto-raw]")) {
    $("c-result").querySelector(".c-raw")?.scrollIntoView({ block: "start", behavior: "smooth" });
    return;
  }
  const ra = e.target.closest("[data-raw-all]");
  if (ra) {
    const id = ra.dataset.rawAll;
    if (craft.rawAll.has(id)) craft.rawAll.delete(id); else craft.rawAll.add(id);
    craftRender(craft.data);
    return;
  }
  const st = e.target.closest("[data-open-steps]");
  if (st) {
    craft.open.add(st.dataset.openSteps);
    craftRender(craft.data);
    $("c-result").querySelector(`tr.main[data-cid="${CSS.escape(st.dataset.openSteps)}"]`)?.scrollIntoView({ block: "start", behavior: "smooth" });
    return;
  }
  if (e.target.closest("a, summary, button")) return;   // 物品名是跳查价页的链接,不当成点行
  const tr = e.target.closest("tr.main[data-cid]");
  if (!tr) return;
  const id = tr.dataset.cid;
  if (craft.open.has(id)) craft.open.delete(id); else craft.open.add(id);
  craftRender(craft.data);
});

function craftHeadHTML(res) {
  const id = res.target, r = refineRole(id), L = craftLine(res.line) || cLine();
  const s = res.sell || {}, v = res.volume || {};
  // +40 是生产加成点数,不是回收率(专精城回收率 36.7%,开专注 53.9%)
  const spec = res.city === L.city ? `${esc(res.city)}(${L.line === "ORE" ? "矿" : L.line === "HIDE" ? "皮" : L.line === "FIBER" ? "纤维" : "木"}的专精城,生产加成 +40)`
    : `${esc(res.city)} <span class="warnish" title="不是这条线的专精城:少 40 点生产加成,回收率明显低">(不是专精城,专精城是 ${esc(L.city)})</span>`;
  const src = x => esc(String(x || "").split("+").map(k => SRC_NAME[k] || k).join(" + "));
  // 挂卖排在卖一附近已经挂着的那些件后面:排队的件数比日成交量多得多,挂上去就要等好几天
  const queue = s.post && (s.post.source === "aodp" ? "卖一附近排了几件不知道(只有 AODP 价)"
    : `卖一附近 ${num(s.post.near_qty)} 件在排队${v.known && v.per_day > 0 ? `(≈ ${(s.post.near_qty / v.per_day).toFixed(1)} 天的成交量)` : ""}`);
  const post = s.post
    ? `<div><b>${num(s.post.best_ask)}</b><i>卖一 · 挂卖到手 ${num(s.post.net)} · ${ageTag(s.post.age_h)}</i><em>${queue} · ${src(s.post.source)}</em></div>`
    : `<div><b>—</b><i>${esc(s.city || "")} 没有卖单</i></div>`;
  const take = s.take
    ? `<div><b>${num(s.take.best_bid)}</b><i>买一 · 吃买单${s.take.max_qty != null ? `断崖前能出 ${fq(s.take.max_qty)} 件` : "(深度未知)"} · ${ageTag(s.take.age_h)}</i><em>${src(s.take.source)}</em></div>`
    : `<div><b>—</b><i>${esc(s.city || "")} 没有买单</i></div>`;
  const vol = v.known
    ? `<div><b>${fq(v.per_day)}</b><i>日均成交(${num(v.days)} 天里 ${num(v.days_with_data)} 天有数据)</i><em>成交历史来自 ${src(v.source)}</em></div>`
    : `<div><b>—</b><i>没有成交量数据</i></div>`;
  return `<div class="c-head">
    <img src="${iconURL(id)}" alt="" onerror="this.style.visibility='hidden'">
    <div class="c-title">
      <h2>${esc(cLabel(id))} ${roleTag(r, true)}</h2>
      <div class="sub">${esc(id)} · 在 ${spec} 精炼 × ${num(res.qty)} 件${s.city && s.city !== res.city ? ` · 在 ${esc(s.city)} 卖` : ""}
        ${r ? ` · 原料:<a href="#lookup/${encodeURIComponent(r.rawID)}">${esc(cLabel(r.rawID))} →</a>` : ""}</div>
    </div>
    <div class="verdict">${post}${take}${vol}</div>
  </div>`;
}

// 数据从哪来、有多旧:新鲜度是硬性要求,不能只给利润
function craftDataHTML(res) {
  const b = res.books || {}, d = res.data || {};
  const parts = [];
  parts.push(`盘口 ${num(b.sides)} 边:抓包 ${num(b.capture)}、只有 AODP 价 ${num(b.aodp)}(深度未知)、两边都没有 ${num(b.empty)}`);
  parts.push(`读抓包窗口 ${num(b.window_hours)} 小时,数据最大年龄 ${num(res.params_used?.sources?.max_age_hours)} 小时`);
  parts.push(`用到的价里最旧的 ${ageTag(d.oldest_age_h)}`);
  const lists = [];
  const line = (label, xs, fmt = x => esc(x)) => { if (xs?.length) lists.push(`<li><b>${label}</b> ${xs.map(fmt).join("、")}</li>`); };
  const side = x => { const [item, city, s] = String(x).split("|"); return `${esc(cLabel(item) || item)} @ ${esc(city || "")}${s ? " " + esc(s) : ""}`; };
  line("缺价:", d.missing, x => esc(cLabel(x) || x));
  line("盘口没翻完、成本只是下界:", d.truncated, side);
  line("只有 AODP 价、不知道件数:", d.depth_unknown, side);
  line("比数据最大年龄还旧、当成缺价:", d.stale, side);
  line("抓包整边太旧、改用了 AODP 价:", b.aodp_over_stale, side);
  line("最近多开串过城、没剔幽灵单:", b.conflicted, side);
  // 串城记录只在服务端内存里:重启前、保留期外的串城看不到,那段时间的盘口照常剔幽灵单
  if (b.conflicted?.length && b.conflicts_since) {
    lists.push(`<li><b>串城检测覆盖自 ${esc(dhm(b.conflicts_since))} 起:</b>比这更早的串城(服务端重启前)看不到</li>`);
  }
  if (b.aodp_newer?.length) line("抓包之后 AODP 报了不同的价(阶梯可能变了):", b.aodp_newer,
    x => `${esc(cLabel(x.item) || x.item)} @ ${esc(x.city)} ${x.side === "sell" ? "卖单" : "买单"} 抓包 ${num(x.capture_price)}(${fq(x.capture_age_h)}h)→ AODP ${num(x.aodp_price)}(${fq(x.aodp_age_h)}h)`);
  if (b.aodp_ok === false) lists.push(`<li><b>AODP 当前价这次没取到:</b>${esc(b.aodp_error || "")}</li>`);
  // 有要紧的事(缺价、串城、AODP 更新……)默认展开;用户自己开合过就照他的
  const open = craft.dataOpen ?? lists.length > 0;
  return `<details class="c-fold c-data"${open ? " open" : ""}>
    <summary>数据 <em>${parts.join(" · ")}</em></summary>
    ${lists.length ? `<ul class="c-list">${lists.join("")}</ul>` : ""}
    ${warnList(res.warnings)}
    <p class="sub">配方来源 ${esc(res.recipe_source || "—")} · 抓包优先,抓包没有的盘口用 AODP 兜底(只有价、没有件数)。${
      b.conflicts_since && !b.conflicted?.length ? ` 串城检测覆盖自 ${esc(dhm(b.conflicts_since))} 起。` : ""}
      件数都是稳态均摊(按期望返还,带小数),进游戏按整数下单。</p>
  </details>`;
}

// ── 事件 ──
$("c-line").addEventListener("click", e => {
  const b = e.target.closest("button[data-line]");
  if (!b || b.dataset.line === craft.p.line) return;
  craft.p.line = b.dataset.line;
  craft.open.clear();
  craftNames(craft.p.line).then(() => { craftRenderGrid(); if (craft.data) craftRender(craft.data); });
  craftChanged({ grid: true });
});
$("c-grid").addEventListener("click", e => {
  const b = e.target.closest("button[data-t]");
  if (!b) return;
  const t = Number(b.dataset.t), en = Number(b.dataset.e);
  if (t === craft.p.tier && en === craft.p.ench) return;
  craft.p.tier = t; craft.p.ench = en;
  craft.p.customTiers = craft.p.customTiers.filter(x => x <= t);
  craft.open.clear();
  craftChanged({ grid: true, now: true });
});
$("c-sell").addEventListener("click", e => {
  const b = e.target.closest("button[data-sell]");
  if (!b || b.dataset.sell === craft.p.sell) return;
  craft.p.sell = b.dataset.sell;
  craftChanged({ grid: true });
});
$("c-run").addEventListener("click", () => craftLoad({ force: true }));
$("c-fce-cal").addEventListener("click", () => {
  $("c-cal").open = true;
  $("c-cal").scrollIntoView({ block: "start", behavior: "smooth" });
});
$("c-reset").addEventListener("click", () => {
  if (!confirm("把高级参数全部恢复成服务端默认?(专精、k、站费、生产加成、跨城来源……都会清掉)")) return;
  const p = craft.p;
  Object.assign(p, {
    fce: {}, unlocked: {}, station: {}, tstation: {}, k: null, kAt: "", kSrc: "", kGold: null, bonus: {},
    island: false, sellCity: "", srcCities: [], transport: null, bid: false, bidMax: null, leftover: "",
    maxAge: null, capital: null, share: null, gold: null, heart: "", confAt: {},
  });
  craftChanged();
});
// 没点过这条线的专精(火法 + 纤维采集,矿 / 皮多半就是这样):一键全填 0,算核对过
$("c-fce-zero").addEventListener("click", () => {
  const p = craft.p;
  p.fce[p.line] = Object.fromEntries(CRAFT_TIERS.map(t => [t, 0]));
  cMark(`fce:${p.line}`, true);
  craftChanged();
});

// 数字框:合法才收进状态;不合法标红、不发请求,离开这个框时换回状态里的值
function cNum(el, lo, hi, opts = {}) {
  const raw = el.value.trim();
  if (raw === "") { el.removeAttribute("aria-invalid"); return { ok: true, v: null }; }
  let v = Number(raw);
  if (!isFinite(v) || v < lo || v > hi || (opts.int && !Number.isInteger(v))) {
    el.setAttribute("aria-invalid", "true");
    el.title = `要在 ${lo} ~ ${hi} 之间${opts.int ? "的整数" : ""}`;
    return { ok: false };
  }
  el.removeAttribute("aria-invalid");
  return { ok: true, v };
}
$("v-craft").addEventListener("input", e => {
  const el = e.target, p = craft.p, city = cCity(), sd = craft.sd || {};
  if (!(el instanceof HTMLInputElement) || el.type !== "number") return;
  let r;
  switch (el.id) {
    case "c-qty": r = cNum(el, 1, 10000, { int: true }); if (r.ok && r.v != null) p.qty = r.v; break;
    case "c-budget": r = cNum(el, 0, sd.focus_cap || CRAFT_FALLBACK.focus_cap); if (r.ok) p.budget = r.v; break;
    case "c-station": r = cNum(el, 0, 1000);
      if (r.ok) { if (r.v == null) delete p.station[city]; else p.station[city] = r.v; cMark(`station:${city}`, r.v != null); }
      break;
    case "c-tstation": r = cNum(el, 0, 1000);
      if (r.ok) { if (r.v == null) delete p.tstation[city]; else p.tstation[city] = r.v; cMark(`tstation:${city}`, r.v != null); }
      break;
    case "c-k": r = cNum(el, 0.5, 3);
      if (r.ok) { p.k = r.v; p.kAt = r.v == null ? "" : cToday(); p.kSrc = r.v == null ? "" : "manual"; p.kGold = r.v == null ? null : craftGold(); }
      break;
    case "c-transport": r = cNum(el, 0, 999); if (r.ok) p.transport = r.v; break;
    case "c-bidmax": r = cNum(el, 0, 1e7); if (r.ok) p.bidMax = r.v; break;
    case "c-maxage": r = cNum(el, 1, sd.window_hours || CRAFT_FALLBACK.window_hours); if (r.ok) p.maxAge = r.v; break;
    case "c-capital": r = cNum(el, 0, 1e13); if (r.ok) p.capital = r.v; break;
    case "c-share": r = cNum(el, 1, 100); if (r.ok) p.share = r.v; break;
    case "c-gold": r = cNum(el, 1, 1e7); if (r.ok) p.gold = r.v; break;
    default:
      if (el.dataset.fce) {
        r = cNum(el, 0, 40000);
        if (r.ok) {
          const f = { ...cFCE() };
          if (r.v == null) delete f[el.dataset.fce]; else f[el.dataset.fce] = r.v;
          p.fce[p.line] = f;
          cMark(`fce:${p.line}`, Object.keys(f).length > 0);
        }
      } else return;
  }
  if (!r.ok) return;
  if (el.id === "c-gold") { craftSave(); craftBadges(); if (craft.data) craftRender(craft.data); return; }   // 金价不影响计算,只影响门槛线
  craftChanged();
});
$("v-craft").addEventListener("change", e => {
  const el = e.target, p = craft.p;
  // 离开数字框:不合法的那个换回状态里的值(状态里一直是上一个合法值)
  if (el instanceof HTMLInputElement && el.type === "number") {
    if (el.getAttribute("aria-invalid")) {
      el.removeAttribute("aria-invalid");
      el.blur();
      craftSyncForm();
    }
    return;
  }
  if (el.tagName === "SELECT") {
    switch (el.id) {
      case "c-city":
        // 选回专精城就当没选:换线时跟着那条线的专精城走
        p.cityBy[p.line] = el.value === cLine().city ? "" : el.value;
        p.srcCities = p.srcCities.filter(c => c !== el.value);
        break;
      case "c-focus":
        p.focusMode = el.value;
        if (el.value === "custom" && !p.customTiers.length) p.customTiers = [p.tier];
        break;
      case "c-sort": p.sort = el.value; break;
      case "c-unlock":
        // 「服务端默认」才是没填;选了「T8(全部)」也记下来,那就是核对过
        if (el.value === "") delete p.unlocked[p.line];
        else p.unlocked[p.line] = Number(el.value);
        cMark(`unlocked:${p.line}`, el.value !== "");
        break;
      case "c-heart": p.heart = el.value; cMark("heart", !!el.value); break;
      case "c-bonus":
        if (el.value === "") delete p.bonus[p.line];
        else p.bonus[p.line] = { v: Number(el.value), d: cToday() };
        break;
      case "c-sellcity": p.sellCity = el.value; break;
      case "c-left": p.leftover = el.value; break;
      default: return;
    }
    craftChanged({ grid: el.id === "c-city" });
    return;
  }
  if (el instanceof HTMLInputElement && el.type === "checkbox") {
    if (el.id === "c-island") p.island = el.checked;
    else if (el.id === "c-bid") p.bid = el.checked;
    else if (el.dataset.src) {
      const c = el.dataset.src;
      p.srcCities = el.checked ? [...new Set([...p.srcCities, c])] : p.srcCities.filter(x => x !== c);
    } else if (el.dataset.ct) {
      const t = Number(el.dataset.ct);
      p.customTiers = el.checked ? [...new Set([...p.customTiers, t])].sort((a, b) => b - a) : p.customTiers.filter(x => x !== t);
    } else return;
    craftChanged();
  }
});

// ── 整条线矩阵(GET /api/refine/matrix):25 档各取最好的一条,替代表格的排行榜 ──
// 矩阵接口只认查询参数里那几项个人参数(k、站费、专注、专精、解锁、出售方式、生产加成、个人岛、
// 数据年龄、本金);跨城买料、挂买单收料、余料估值、成交份额按服务端默认 —— 页面上写明
const mx = { data: null, sig: "", seq: 0, pending: false, err: null, sort: { key: "level", dir: "asc" }, qty: null };
function craftMatrixQuery() {
  const p = craft.p, body = craftBody();
  const q = new URLSearchParams({ line: p.line, city: body.city, qty: String(mx.qty || p.qty) });
  const f = body.fees;
  if (f.transmute_factor != null) q.set("k", String(f.transmute_factor));
  if (f.station_per100 != null) q.set("station_per100", String(f.station_per100));
  if (f.transmute_station_per100 != null) q.set("transmute_station_per100", String(f.transmute_station_per100));
  if (body.focus.budget != null) q.set("budget", String(body.focus.budget));
  q.set("fce", CRAFT_TIERS.map(t => `${t}:${body.focus.fce[t] || 0}`).join(","));
  if (body.focus.unlocked_tier != null) q.set("unlocked_tier", String(body.focus.unlocked_tier));
  // 矩阵不支持自选哪几级:每一格的链长短不一样,按「自动」算
  q.set("focus", p.focusMode === "custom" ? "auto" : p.focusMode);
  q.set("sort", p.sort);
  q.set("sell", p.sell);
  if (body.rrr.daily_bonus != null) q.set("daily_bonus", String(body.rrr.daily_bonus));
  if (p.island) q.set("island", "1");
  if (body.sources.max_age_hours != null) q.set("max_age_hours", String(body.sources.max_age_hours));
  if (body.daily.capital != null) q.set("capital", String(body.daily.capital));
  // 心返还矩阵不收(按服务端默认算),所以它不能跟着标「核对过」;矩阵一直算到 T8,
  // 专精要五级都填了才算
  const fce = cFCE(), fceAll = CRAFT_TIERS.every(t => typeof fce[t] === "number");
  const conf = (body.confirmed || []).filter(k => k !== "heart_returns" && (k !== "focus.fce" || fceAll));
  if (conf.length) q.set("confirmed", conf.join(","));
  return q.toString();
}
async function craftMatrixLoad(opts = {}) {
  if (!$("c-mx").open) return;
  const qs = craftMatrixQuery();
  if (!opts.force && qs === mx.sig && (mx.pending || mx.data)) return;
  mx.sig = qs;
  const seq = ++mx.seq;
  mx.pending = true;
  mx.err = null;
  craftMatrixRender();
  try {
    const res = await getJSON("/api/refine/matrix?" + qs);
    if (seq !== mx.seq) return;
    mx.data = res;
    if (res.server_defaults) craft.sd = res.server_defaults;
  } catch (e) {
    if (seq !== mx.seq) return;
    mx.err = e;
    mx.sig = "";
  }
  mx.pending = false;
  craftMatrixRender();
}
const MX_COLS = [
  { k: "level", label: "档位", l: true },
  { k: "label", label: "最好的路线", l: true, str: true },
  { k: "unit_cost", label: "单根成本" },
  { k: "profit", label: "单根利润", tip: "主出售方式下每件的利润" },
  { k: "pct", label: "利润率" },
  { k: "spf", label: "银/专注点", tip: "(开专注的利润 − max(0, 不开专注的最好利润))÷ 每件专注:不开专注本来就亏的话,按什么都不做(0)当基准" },
  { k: "volume", label: "日成交量", tip: "出售城市的日均成交件数(总量 ÷ 自然日)" },
  { k: "daily", label: "日化", tip: "每天件数 × 单根利润;每天件数取专注、成交量、本金、这一批件数里最小的" },
  { k: "binding", label: "卡在", l: true, str: true },
  { k: "age", label: "数据", tip: "用到的价里最旧的" },
];
function mxRows(res) {
  const m0 = res.params_used?.sell?.modes?.[0] || "post";
  return (res.cells || []).map(c => {
    const b = c.best, pl = b?.profit?.[m0];
    return {
      c, b, target: c.target, level: c.tier * 10 + c.enchant,
      label: b?.label || c.error || "",
      unit_cost: b?.unit_cost ?? null, profit: pl?.unit ?? null, pct: pl?.pct ?? null,
      spf: b?.silver_per_focus ?? null, volume: c.volume?.known ? c.volume.per_day : null,
      daily: b?.daily?.profit ?? null, binding: b?.daily ? (BIND_WORD[b.daily.binding] || b.daily.binding) : "",
      age: b?.oldest_age_h ?? c.data?.oldest_age_h ?? null,
    };
  });
}
function craftMatrixRender() {
  const box = $("c-mx-body");
  const res = mx.data, L = cLine();
  $("c-mx-sum").textContent = mx.pending ? "正在算 25 档…(每档一次完整计算,第一次要几秒)"
    : res ? `${res.line === L.line ? "" : "(还是上一条线的)"}${res.city} · 每档 ${num(res.qty)} 件 · 点一行切到那个目标`
    : "点开算这条线全部 25 档";
  if (mx.err) {
    box.innerHTML = `<p class="c-status bad">${mx.err.status === 504 ? "服务端还没算完(多半在等 AODP),计算在后台继续,过几秒点「重算这张表」。" : esc(mx.err.message || mx.err)}</p>
      <button class="act ghost" data-mx-reload>重算这张表</button>`;
    return;
  }
  if (!res) { box.innerHTML = mx.pending ? `<p class="c-status busy">正在算…</p>` : ""; return; }
  const rows = mxRows(res);
  const { key, dir } = mx.sort;
  const col = MX_COLS.find(c => c.k === key) || MX_COLS[0];
  rows.sort((a, b) => {
    const x = a[key], y = b[key];
    // 算不出来的(缺卖价、不用专注)永远排最后,不管升序降序
    if ((x == null || x === "") !== (y == null || y === "")) return x == null || x === "" ? 1 : -1;
    const cmp = col.str ? String(x).localeCompare(String(y), "zh-CN") : (x - y);
    return (dir === "desc" ? -cmp : cmp) || a.level - b.level;
  });
  const th = craftPremiumSPF(), cur = cTarget(), qtyBound = rows.filter(r => r.b?.daily?.binding === "qty").length;
  const m0 = res.params_used?.sell?.modes?.[0] || "post";
  const typing = document.activeElement?.id === "c-mx-qty";   // 正在改件数:重画完把焦点还回去
  box.innerHTML = `<div class="c-row c-mxbar">
      <div class="ctrl"><label for="c-mx-qty">每档按几件算</label>
        <input type="number" id="c-mx-qty" min="1" max="10000" step="1" value="${esc(mx.qty || "")}" placeholder="${esc(craft.p.qty)}"></div>
      <button class="act ghost" data-mx-reload>重算这张表</button>
      <span class="sub">利润按${MODE_WORD[m0]}算;专注${craft.p.focusMode === "custom" ? "按「自动」(矩阵不支持自选哪几级)" : "照参数条"};
        跨城买料、挂买单收料、余料估值、成交份额按服务端默认。${qtyBound ? `有 ${qtyBound} 档的日化卡在「这一批的件数」:单根成本只按这个件数沿阶梯算过,想看真实日产就把件数调大。` : ""}</span>
    </div>
    <div class="tablewrap"><table class="tight c-mxt">
      <thead><tr>${MX_COLS.map(c => `<th data-mxk="${c.k}" class="s${c.l ? " l" : ""}"${c.k === key ? ` data-dir="${dir}"` : ""}${c.tip ? ` title="${esc(c.tip)}"` : ""}>${c.label}</th>`).join("")}<th class="l">警告</th></tr></thead>
      <tbody>${rows.map(r => {
        const ws = new Map();
        for (const w of [...(r.c.warnings || []), ...(r.b?.warnings || [])]) if (!ws.has(w.code)) ws.set(w.code, w);
        return `<tr class="main${r.target === cur ? " cur" : ""}${r.b && !r.b.filled ? " unfilled" : ""}" data-mxt="${esc(r.target)}">
          <td class="l"><b>${esc(cTier(r.target))}</b> <span class="sub">${esc(cName(r.target))}</span></td>
          <td class="l">${esc(r.label || "—")}</td>
          <td>${num(r.unit_cost)}</td>
          <td class="${r.profit == null ? "" : r.profit >= 0 ? "pos" : "neg"}">${sgn(r.profit)}</td>
          <td>${spct(r.pct)}</td>
          <td class="${r.spf != null && r.spf >= th ? "pos" : ""}">${snum(r.spf)}</td>
          <td>${r.volume == null ? `<span class="sub" title="没有成交量数据">—</span>` : fq(r.volume)}</td>
          <td class="${r.daily == null ? "" : r.daily >= 0 ? "pos" : "neg"}"><b>${sgn(r.daily)}</b></td>
          <td class="l">${esc(r.binding || "—")}</td>
          <td>${ageTag(r.age)}</td>
          <td class="l wcell">${[...ws.values()].map(warnTag).join("")}</td>
        </tr>`;
      }).join("")}</tbody>
    </table></div>`;
  if (typing) $("c-mx-qty").focus();
}
$("c-mx").addEventListener("toggle", () => { if ($("c-mx").open) { craftMatrixRender(); craftMatrixLoad(); } });
$("c-mx-body").addEventListener("click", e => {
  if (e.target.closest("[data-mx-reload]")) { craftMatrixLoad({ force: true }); return; }
  const th = e.target.closest("th[data-mxk]");
  if (th) {
    const k = th.dataset.mxk;
    if (mx.sort.key === k) mx.sort.dir = mx.sort.dir === "desc" ? "asc" : "desc";
    else mx.sort = { key: k, dir: k === "level" || MX_COLS.find(c => c.k === k)?.str ? "asc" : "desc" };
    craftMatrixRender();
    return;
  }
  const tr = e.target.closest("tr[data-mxt]");
  if (!tr || e.target.closest("a")) return;
  const r = refineRole(tr.dataset.mxt);
  if (!r) return;
  craft.p.tier = r.tier; craft.p.ench = r.ench;
  craft.open.clear();
  craftChanged({ grid: true, now: true });
  craftMatrixRender();
  window.scrollTo({ top: 0, behavior: "smooth" });
});
$("c-mx-body").addEventListener("input", e => {
  if (e.target.id !== "c-mx-qty") return;
  const r = cNum(e.target, 1, 10000, { int: true });
  if (!r.ok) return;
  mx.qty = r.v;
  clearTimeout(mx.timer);
  mx.timer = setTimeout(() => craftMatrixLoad(), 500);
});

// ── 配方树(GET /api/refine/recipe):只读服务端内存里的配方表,不读库、不打 AODP ──
const craftTrees = new Map();   // 物品 id → Promise<树>
function craftTree(id) {
  if (!craftTrees.has(id)) {
    const p = getJSON("/api/refine/recipe?item=" + encodeURIComponent(id));
    p.catch(() => craftTrees.delete(id));   // 失败了下次重拉,不把失败缓存住
    craftTrees.set(id, p);
  }
  return craftTrees.get(id);
}

// ── 校准:从游戏里读数反推 k 和专精(design-questions 第 1、2 条) ──
// 服务端的转化实付 = 基础银币 × k + 产物物品价值 × 0.1125 × 主人费率 / 100(规则表 B5、B6)。
// 游戏确认框的明细是 基本花费 − 全局折扣 + 主人费用 + 地区税费:全局折扣(金价低于 5000 才有)
// 和地区税费没有单独的模型,和服务端一样都归进 k;主人费用单独反推成主人费率
const ITEM_VALUE_TO_NUTRITION = 0.1125;   // 规则表 E1 ✔
const FCE_HALVING = 10000;                // 规则表 D2 ✔:每 10000 点 FCE 专注消耗减半
const FCE_MAX = 40000;                    // 规则表 D3 ✔
const STATION_WORD = { ORE: "冶炼厂", HIDE: "制革厂" };
const craftCal = { edge: "", qty: 1, base: null, disc: null, owner: null, region: null, fItem: "", shown: null, levels: {} };

async function craftRenderCal() {
  const box = $("c-cal-body"), L = cLine();
  const rawTop = refineID(L.raw, 8, 4);   // 转化格子取整条线(T8.4 原料的全部祖先),哪一格都能量 k
  let rawTree = null, tgtTree = null;
  try { [rawTree, tgtTree] = await Promise.all([craftTree(rawTop), craftTree(cTarget())]); } catch (e) {
    box.innerHTML = `<p class="c-status bad">配方树没拉到:${esc(e.message || e)}</p>`;
    return;
  }
  if (!$("c-cal").open) return;
  const edges = rawTree.edges || [];
  const defEdge = `${refineID(L.raw, 8, 3)}>${rawTop}`;
  if (!edges.some(e => `${e.from}>${e.to}` === craftCal.edge)) craftCal.edge = defEdge;
  const chain = (tgtTree.chain || []).filter(n => n.makeable && n.recipes?.length);
  if (!chain.some(n => n.item === craftCal.fItem)) craftCal.fItem = chain[0]?.item || "";
  const station = STATION_WORD[L.line] || "精炼站";
  const v = x => (x == null ? "" : String(x));
  box.innerHTML = `
    <fieldset><legend>转化系数 k 和转化主人费率</legend>
      <ol class="c-how">
        <li>在 ${esc(L.city)} 的 T8 ${station}打开「转化」分页,选 <b>T8.3 → T8.4 ${esc(cName(rawTop) || "原料")}</b>、数量 1。</li>
        <li>打开确认弹窗里的「计算 / 明细」,把<b>基本花费 / 全局折扣 / 主人费用 / 地区税费</b>四行照抄到下面。</li>
        <li>可以的话再量一格 <b>T7.0 → T8.0</b>(在下拉里换一格):两格算出来的 k 应该差不多,差得多说明抄错了或者 k 不是一个常数。</li>
      </ol>
      <p class="sub">k 直接决定「原料直接买」还是「从低级转上来」:差一点就可能翻转结论。k 以前变过(随版本、随时间),量的时候记下日期。</p>
      <div class="c-row">
        <div class="ctrl"><label for="cal-edge">转化哪一格</label>
          <select id="cal-edge">${edges.map(e => `<option value="${esc(e.from + ">" + e.to)}">${esc(cTier(e.from))} → ${esc(cTier(e.to))}(基础 ${num(e.silver)})</option>`).join("")}</select></div>
        <div class="ctrl"><label for="cal-qty">数量</label><input type="number" id="cal-qty" min="1" step="1" value="${v(craftCal.qty)}"></div>
        <div class="ctrl"><label for="cal-base">基本花费</label><input type="number" id="cal-base" min="0" step="1" value="${v(craftCal.base)}"></div>
        <div class="ctrl"><label for="cal-disc">全局折扣</label><input type="number" id="cal-disc" min="0" step="1" value="${v(craftCal.disc)}" placeholder="0"></div>
        <div class="ctrl"><label for="cal-owner">主人费用</label><input type="number" id="cal-owner" min="0" step="1" value="${v(craftCal.owner)}" placeholder="0"></div>
        <div class="ctrl"><label for="cal-region">地区税费</label><input type="number" id="cal-region" min="0" step="1" value="${v(craftCal.region)}" placeholder="0"></div>
      </div>
      <div class="c-calout" id="cal-k-out"></div>
    </fieldset>
    <fieldset><legend>精炼专精 FCE(${esc(L.line === "ORE" ? "矿石" : L.line === "HIDE" ? "兽皮" : L.line === "FIBER" ? "纤维" : "木材")}精炼)</legend>
      <ol class="c-how">
        <li>最快:在${station}里选中 <b>${esc(cLabel(chain[0]?.item || cTarget()))}</b>,看显示的<b>专注消耗</b>是多少,填到下面「方法一」。</li>
        <li>或者:命运之轮里 T4~T8 五个精炼节点各自的等级,填到「方法二」,五个 tier 一起算出来。</li>
      </ol>
      <p class="sub">专注消耗 = 基础 × 0.5^(FCE ÷ 10000)(规则表 D2)。0 专精和满专精差 16 倍,每点专注值多少银、每天能做几件都取决于它。</p>
      <div class="c-row">
        <div class="ctrl"><label for="cal-fitem">方法一:哪一件</label>
          <select id="cal-fitem">${chain.map(n => `<option value="${esc(n.item)}">${esc(cLabel(n.item))}(基础专注 ${num(n.recipes[0].focus)})</option>`).join("")}</select></div>
        <div class="ctrl"><label for="cal-shown">游戏里显示的专注消耗</label><input type="number" id="cal-shown" min="1" step="1" value="${v(craftCal.shown)}"></div>
      </div>
      <div class="c-calout" id="cal-f-out"></div>
      <div class="c-row">${CRAFT_TIERS.map(t => `<div class="ctrl"><label for="cal-lv-${t}">方法二:T${t} 节点等级</label>
        <input type="number" id="cal-lv-${t}" data-lv="${t}" min="0" max="100" step="1" value="${v(craftCal.levels[t])}" placeholder="0"></div>`).join("")}</div>
      <div class="c-calout" id="cal-l-out"></div>
    </fieldset>`;
  $("cal-edge").value = craftCal.edge;
  $("cal-fitem").value = craftCal.fItem;
  craftCal.edges = edges;
  craftCal.chain = chain;
  craftCalCompute();
}

function craftCalCompute() {
  const edges = craftCal.edges || [], chain = craftCal.chain || [];
  const out = $("cal-k-out");
  if (!out) return;
  // ① k
  const e = edges.find(x => `${x.from}>${x.to}` === craftCal.edge);
  const n = craftCal.qty > 0 ? craftCal.qty : 1;
  if (!e || craftCal.base == null) {
    out.innerHTML = `<span class="sub">填上「基本花费」就能算。这一格的基础价(配方数据)是 ${e ? num(e.silver) : "—"},产物物品价值 ${e ? fq(e.to_iv) : "—"}。</span>`;
  } else {
    const disc = craftCal.disc || 0, owner = craftCal.owner || 0, region = craftCal.region || 0;
    const paid = craftCal.base - disc + owner + region;
    const k = (craftCal.base - disc + region) / (n * e.silver);
    const fee = e.to_iv > 0 ? owner / (n * e.to_iv * ITEM_VALUE_TO_NUTRITION) * 100 : null;
    const kOK = isFinite(k) && k >= 0.5 && k <= 3;
    const feeOK = fee != null && isFinite(fee) && fee >= 0 && fee <= 1000;
    craftCal.k = kOK ? Math.round(k * 10000) / 10000 : null;
    craftCal.fee = feeOK ? Math.round(fee) : null;
    out.innerHTML = `实付合计 ${num(paid)}(${num(n)} 次)。
      <b>k = (${num(craftCal.base)} − ${num(disc)} + ${num(region)}) ÷ (${num(n)} × ${num(e.silver)}) = ${kOK ? k.toFixed(4) : "—"}</b>
      ${kOK ? "" : `<span class="warnish">算出来不在 0.5~3 之间,多半抄错了一行或数量不对</span>`}
      · 主人费率 ≈ ${num(owner)} ÷ (${num(n)} × ${fq(e.to_iv)} × 0.1125) × 100 = <b>${feeOK ? num(fee) : "—"}</b> 银/100 营养
      <div class="c-row"><button class="act" id="cal-k-apply"${kOK ? "" : " disabled"}>写入 k(记今天 ${cToday()})</button>
        <button class="act ghost" id="cal-fee-apply"${feeOK ? "" : " disabled"}>写入主人费率(${esc(cCity())})</button></div>`;
  }
  // ② 按显示的专注消耗反推 FCE
  const node = chain.find(x => x.item === craftCal.fItem);
  const fOut = $("cal-f-out");
  const base = node?.recipes?.[0]?.focus;
  if (!node || !base || !craftCal.shown) {
    fOut.innerHTML = `<span class="sub">填上显示的专注消耗就能算${base ? `(这一件基础 ${num(base)},满专精是 ${fq(base / 16)})` : ""}。</span>`;
    craftCal.fce = null;
  } else {
    const s = craftCal.shown;
    const f = x => FCE_HALVING * Math.log2(base / x);
    // 游戏里显示的是整数:真实值落在 [s − 0.5, s + 0.5) 里,反推出来是一个区间
    const lo = Math.max(0, f(s + 0.5)), hi = Math.min(FCE_MAX, s > 0.5 ? f(s - 0.5) : FCE_MAX);
    const mid = Math.min(FCE_MAX, Math.max(0, f(s)));
    craftCal.fce = Math.round(mid);
    craftCal.fTier = node.tier;
    const odd = s > base ? "比基础值还高:是不是选错了物品?" : s < base / 16 - 0.5 ? "比满专精(1/16)还低:是不是看错了?" : "";
    fOut.innerHTML = `<b>T${node.tier} 的 FCE ≈ ${num(mid)}</b>(显示是整数,真实值在 ${num(lo)} ~ ${num(hi)} 之间)
      ${odd ? `<span class="warnish">${odd}</span>` : ""}
      <div class="c-row"><button class="act" id="cal-f-apply">写入 T${node.tier} 专精</button></div>`;
  }
  // ③ 按命运之轮节点等级算五个 tier:每级给整条线 +30、给本 tier 另加 +250(规则表 D3 ✔)
  const lv = CRAFT_TIERS.map(t => Math.max(0, Math.min(100, Number(craftCal.levels[t]) || 0)));
  const sum = lv.reduce((a, b) => a + b, 0);
  const fces = CRAFT_TIERS.map((t, i) => Math.min(FCE_MAX, 30 * sum + 250 * lv[i]));
  craftCal.fces = fces;
  $("cal-l-out").innerHTML = `FCE = 30 × 五个节点等级之和(${num(sum)})+ 250 × 本 tier 等级:${
    CRAFT_TIERS.map((t, i) => `T${t} <b>${num(fces[i])}</b>`).join(" · ")}
    <div class="c-row"><button class="act ghost" id="cal-l-apply"${sum > 0 ? "" : " disabled"}>写入全部五个 tier</button></div>`;
}

function craftCalSummary() {
  const p = craft.p, fce = cFCE();
  const parts = [p.k != null ? `k=${p.k}(${p.kSrc === "calib" ? "实测" : "手填"} ${p.kAt || ""})` : "k 未核实"];
  const set = CRAFT_TIERS.filter(t => fce[t] > 0);
  parts.push(set.length ? `专精 ${set.map(t => `T${t} ${num(fce[t])}`).join("、")}`
    : cFCEFull() ? "专精全是 0(你填的)" : "专精没填(按 0 算)");
  $("c-cal-sum").textContent = parts.join(" · ");
}

$("c-cal").addEventListener("toggle", () => { if ($("c-cal").open) craftRenderCal(); });
$("c-cal-body").addEventListener("input", e => {
  const el = e.target;
  const val = el.value.trim() === "" ? null : Number(el.value);
  const ok = val == null || isFinite(val);
  if (!ok) return;
  switch (el.id) {
    case "cal-qty": craftCal.qty = val || 1; break;
    case "cal-base": craftCal.base = val; break;
    case "cal-disc": craftCal.disc = val; break;
    case "cal-owner": craftCal.owner = val; break;
    case "cal-region": craftCal.region = val; break;
    case "cal-shown": craftCal.shown = val; break;
    default:
      if (el.dataset.lv) craftCal.levels[el.dataset.lv] = val;
      else return;
  }
  craftCalCompute();
});
$("c-cal-body").addEventListener("change", e => {
  if (e.target.id === "cal-edge") { craftCal.edge = e.target.value; craftCalCompute(); }
  if (e.target.id === "cal-fitem") { craftCal.fItem = e.target.value; craftCalCompute(); }
});
$("c-cal-body").addEventListener("click", e => {
  const b = e.target.closest("button[id]");
  if (!b || b.disabled) return;
  const p = craft.p;
  if (b.id === "cal-k-apply" && craftCal.k != null) {
    Object.assign(p, { k: craftCal.k, kAt: cToday(), kSrc: "calib", kGold: craftGold() });
  } else if (b.id === "cal-fee-apply" && craftCal.fee != null) {
    p.tstation[cCity()] = craftCal.fee;
    cMark(`tstation:${cCity()}`, true);
  } else if (b.id === "cal-f-apply" && craftCal.fce != null) {
    p.fce[p.line] = { ...cFCE(), [craftCal.fTier]: craftCal.fce };
    cMark(`fce:${p.line}`, true);
  } else if (b.id === "cal-l-apply" && craftCal.fces) {
    const f = {};
    CRAFT_TIERS.forEach((t, i) => { f[t] = craftCal.fces[i]; });
    p.fce[p.line] = f;
    cMark(`fce:${p.line}`, true);
  } else return;
  b.textContent = "已写入 ✓";
  b.disabled = true;
  craftChanged();
});

// ── 页面钩子(app.js 的 show / route 调) ──
function craftRoute(id) {
  const r = refineRole(id);
  if (!r || !r.craftable) return;
  // 原料进来就算它炼成的那个半成品(查价页原料上的「去精炼计算」)
  const changed = craft.p.line !== r.line || craft.p.tier !== r.tier || craft.p.ench !== r.ench;
  craft.p.line = r.line; craft.p.tier = r.tier; craft.p.ench = r.ench;
  if (changed) { craft.open.clear(); craftSave(); }
}
function craftShow() {
  craftRenderGrid();
  craftSyncForm();
  craftNames(craft.p.line).then(() => { craftRenderGrid(); if (craft.data) craftRender(craft.data); });
  // 回到这页:照上次的树重新订阅;不在这页时退订了实时,手上那份可能旧了,隔得久就强制重算一次
  if (craft.keys.size) rtSetKeys("craft", [...craft.keys], { snapshot: false });
  craftLoad({ force: !!craft.data && Date.now() - craft.loadedAt > 15000 });
}
// 离开这页:退订(设计 §4.5),攒着的实时重算也取消。keys 留着,回来时照它重新订
function craftHide() {
  clearTimeout(craft.timer);
  clearTimeout(craft.liveTimer);
  craft.liveTimer = 0;
  craft.liveFirst = 0;
  rtSetKeys("craft", []);
}
viewHooks.craft = {
  hash: () => "craft/" + encodeURIComponent(cTarget()),
  route: craftRoute, show: craftShow, hide: craftHide,
};
