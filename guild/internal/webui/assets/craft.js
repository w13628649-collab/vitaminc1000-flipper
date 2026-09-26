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
  },
  sd: null,          // 服务端默认值(响应里的 server_defaults)
  data: null,        // 最近一次 plan 响应
  sig: "",           // 最近一次请求体:同样的参数短时间内不重复请求
  loadedAt: 0, seq: 0, pending: false, timer: 0, retry: 0, err: null,
  open: new Set(),   // 候选表里展开着的行(按候选 id)
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
// 件数:稳态消耗是小数(按期望返还算),整数就不带小数点
const fq = q => (q == null || !isFinite(q) ? "—" : Math.abs(q - Math.round(q)) < 0.005 ? num(q) : q.toFixed(2));
const sgn = n => (n == null || !isFinite(n) ? "—" : (n > 0 ? "+" : n < 0 ? "−" : "") + num(Math.abs(n)));
// 可能是负数、但不需要 + 号的数(每点专注值):负号统一用 −
const snum = n => (n == null || !isFinite(n) ? "—" : (n < 0 ? "−" : "") + num(Math.abs(n)));
// 带正负号的百分比(利润率、和直接买比贵多少):减号和 sgn 一样用 −
const spct = (n, d = 1) => (n == null || !isFinite(n) ? "—" : (n > 0 ? "+" : n < 0 ? "−" : "") + (Math.abs(n) * 100).toFixed(d) + "%");
const hms = t => {
  const d = t instanceof Date ? t : new Date(t);
  return isFinite(d) ? d.toLocaleTimeString("zh-CN", { hour12: false }) : "—";
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
const warnTag = w => `<span class="wtag${CRAFT_WARN_BAD.has(w.code) ? " bad" : ""}" title="${esc(w.msg || w.code)}">${
  esc(CRAFT_WARN[w.code] || w.code)}</span>`;
function warnList(ws) {
  if (!ws?.length) return "";
  return `<ul class="c-warns">${ws.map(w => `<li>${warnTag(w)} ${esc(w.msg || "")}</li>`).join("")}</ul>`;
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
  $("c-unlock").value = String(p.unlocked[p.line] ?? 8);
  setVal("c-station", p.station[city]);
  setPh("c-station", sd.station_fee_by_city?.[city] ?? sd.station_fee_per100);
  setVal("c-k", p.k);
  setPh("c-k", sd.transmute_factor);
  setVal("c-tstation", p.tstation[city]);
  setPh("c-tstation", sd.transmute_station_fee_per100);
  const bonus = cBonus();
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
  setPh("c-share", 30);
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
  const st = p.station[city];
  badge("c-station-badge", st != null, st != null ? "你填的" : "未核实",
    st != null ? `${city} 的精炼站费按你填的 ${st} 算` : "服务端默认的站费是表格作者用的数,没核实。进城按 N 看地图,悬停在精炼站上");
  const ts = p.tstation[city];
  badge("c-tstation-badge", ts != null, ts != null ? "你填的" : "未核实",
    ts != null ? `${city} 的转化主人费率按你填的 ${ts} 算` : "服务端默认值,没核实。校准里填主人费用可以反推");
  const b = cBonus(), stale = craft.p.bonus[craft.p.line] && b == null;
  badge("c-bonus-badge", b != null, b != null ? `今天 · ${cToday()}` : stale ? "昨天填的,已作废" : "未核实",
    "每日生产加成每天换品类,只能看游戏里的「活动」窗口手填;只对填的那一天有效");
  // 高级区的摘要:折起来也看得到改过哪些
  const mine = [];
  if (Object.values(cFCE()).some(v => v > 0)) mine.push("专精");
  if (p.k != null) mine.push("k");
  if (st != null) mine.push("站费");
  if (ts != null) mine.push("主人费率");
  if (b != null) mine.push("生产加成");
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
    craft.data = res;
    craft.loadedAt = Date.now();
    if (res.server_defaults) craft.sd = res.server_defaults;
    craft.tries = 0;
    craftSyncForm();
    craftRender(res);
    craftStatus("ok", opts);
    craftAfterPlan(res, opts);
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
}
// 拿到计划之后的事:实时订阅、矩阵等在后面的提交里接上
function craftAfterPlan(res, opts) { /* 见下文 */ }

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
  el.innerHTML = `算于 ${hms(at)}${craft.liveNote ? ` · <b class="live">${craft.liveNote}</b>` : ""}`;
}

// ── 结果 ──
function craftRender(res) {
  $("c-result").classList.remove("dim");
  $("c-result").innerHTML = craftHeadHTML(res) + craftCardHTML(res) + craftCandsHTML(res) + craftDataHTML(res);
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
  const net = m0 === "take"
    ? (s.take ? `<b>${num(s.take.fill?.net_avg)}</b><i>吃买单均价 ${num(s.take.fill?.vwap)} × 0.96;断崖前出得掉 ${fq(s.take.fill?.got)} 件</i>` : `<b>—</b><i>没有买单</i>`)
    : (s.post ? `<b>${num(s.post.net)}</b><i>挂 ${num(s.post.price)}(卖一 ${num(s.post.best_ask)} 压 1 银),扣 税 + 创建费</i>` : `<b>—</b><i>没有卖单</i>`);
  const profitLine = (pl, mode) => !pl ? `${MODE_WORD[mode]} —`
    : `${MODE_WORD[mode]} <span class="${pl.unit >= 0 ? "pos" : "neg"}">${sgn(pl.unit)}(${spct(pl.pct)})</span>${
      pl.sold != null && pl.sold < res.qty - 0.005 ? `,只出得掉 ${fq(pl.sold)} 件` : ""}`;
  const d = c.daily;
  const unv = res.params_used?.unverified || [];
  const unvHTML = unv.map(u => {
    const val = typeof u.value === "object" && u.value
      ? Object.entries(u.value).map(([t, f]) => `T${t} ${num(f)}`).join(" ")
      : typeof u.value === "boolean" ? (u.value ? "是" : "否")
      : u.key === "focus.unlocked_tier" ? `T${u.value}`
      : u.key === "rrr.daily_bonus" ? `+${u.value}`
      : /station_per100$/.test(u.key) ? `${num(u.value)} 银/100 营养` : String(u.value);
    return `<li class="${u.is_default ? "def" : "mine"}" title="${esc(`规则表 ${u.rule}。怎么核对:${u.check}`)}">
      <b>${esc(UNVERIFIED_WORD[u.key] || u.key)}</b> ${esc(val)} <em>${u.is_default ? "默认值,没核实" : "你填的"}</em></li>`;
  }).join("");
  const f = c.focus || {};
  return `<section class="c-card">
    <div class="c-card-head">
      <div><span class="c-kicker">推荐</span> <b class="c-route">${esc(c.label)}</b>
        ${c.filled ? "" : `<span class="wtag bad" title="深度不够或缺价:成本只按拿得到的那部分算">拿不满料</span>`}</div>
      <button class="act ghost" data-open-steps="${esc(c.id)}">看步骤清单 ↓</button>
    </div>
    <div class="kpis c-kpis">
      <div><b>${num(c.unit_cost)}</b><i>单根成本</i><em>这一批 ${num(res.qty)} 件共 ${num(c.total_cost)}</em></div>
      <div>${net}</div>
      <div><b class="${p0 && p0.unit >= 0 ? "pos" : "neg"}">${p0 ? sgn(p0.unit) : "—"}</b><i>单根利润(${MODE_WORD[m0]})· 利润率 ${p0 ? spct(p0.pct) : "—"}</i>
        <em>这一批 ${p0 ? sgn(p0.total) : "—"} · ${m1 ? profitLine(p1, m1) : ""}</em></div>
      <div><b>${c.silver_per_focus == null ? "—" : snum(c.silver_per_focus)}</b><i>每点专注值(银)</i>${spfBar(c.silver_per_focus)}</div>
      <div title="${esc(limitsText(d))}"><b class="${d && d.profit >= 0 ? "pos" : "neg"}">${d ? sgn(d.profit) : "—"}</b><i>日化收益 / 天</i>
        <em>${d ? `每天 ${fq(d.units)} 件 · 卡在<b>${esc(BIND_WORD[d.binding] || d.binding)}</b>` : "算不出(没有卖价)"}</em></div>
      <div class="${over ? "c-over" : ""}"><b>${num(c.upfront_capital)}</b><i>开工本金(含批末余料)</i>
        <em>${cap > 0 ? (over ? `<b class="neg">超过本金 ${num(cap)}(${(c.upfront_capital / cap).toFixed(1)} 倍)</b>:一次备不齐,把件数调小或分批` : `本金 ${num(cap)} 以内`) : "不按本金封顶"}</em></div>
      <div><b>${ageTag(c.oldest_age_h)}</b><i>用到的价里最旧的</i><em>${c.age_level === "red" ? "超过 24 小时,先进游戏看一眼" : c.age_level === "yellow" ? "超过 6 小时,下单前核一下" : "6 小时以内"}</em></div>
    </div>
    <p class="c-focusln">专注:${f.tiers?.length ? `T${f.tiers.join("、T")} 开,每根 ${num(f.per_unit)}(没有专精时 ${num(f.base_per_unit)})· 这一批 ${num(f.effective)}` : "不开"}
      · 今日可用 ${num(res.focus?.budget)}${f.over_budget ? ` <span class="wtag bad">这一批要的专注超过今日可用</span>` : ""}</p>
    ${unv.length ? `<div class="c-unv"><span>用到的<b>未核实参数</b>:</span><ul>${unvHTML}</ul>
      <button class="act ghost mini-act" data-open-cal>去校准</button></div>` : ""}
    ${warnList(c.warnings)}
  </section>`;
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
    const profitCell = (pl, mode) => pl
      ? `<td class="${pl.unit >= 0 ? "pos" : "neg"}${mode === modes[0] ? " primary" : ""}">${sgn(pl.unit)}<span class="subln">${spct(pl.pct)}${
        pl.sold != null && pl.sold < res.qty - 0.005 ? ` · 出 ${fq(pl.sold)} 件` : ""}</span></td>`
      : `<td class="sub">—</td>`;
    const codes = new Map();
    for (const w of c.warnings || []) if (!codes.has(w.code)) codes.set(w.code, w);
    return `<tr class="main${c.reference ? " refrow" : ""}${c.filled ? "" : " unfilled"}${open ? " open" : ""}" data-cid="${esc(c.id)}">
      <td class="l">${c.reference ? `<span class="kind flip">参照</span> ` : `<span class="rank">${i + 1}</span> `}<span class="c-route">${esc(c.label)}</span>
        ${c.reference ? `<span class="subln">${esc(REF_NOTE[c.reference] || "参照行")}</span>` : ""}</td>
      <td>${num(c.unit_cost)}</td>
      ${profitCell(pp, "post")}${profitCell(pt, "take")}
      <td class="${f.over_budget ? "neg" : ""}" title="${esc(f.tiers?.length ? `开专注的级:T${f.tiers.join("、T")};没有专精时每根 ${num(f.base_per_unit)}${f.over_budget ? ";这一批超过今日可用专注" : ""}` : "不开专注")}">${f.per_unit ? num(f.per_unit) : "0"}</td>
      <td class="${c.silver_per_focus != null && c.silver_per_focus >= th ? "pos" : c.silver_per_focus < 0 ? "neg" : ""}">${c.silver_per_focus == null ? "—" : snum(c.silver_per_focus)}</td>
      <td title="${esc(limitsText(d))}">${d ? sgn(d.profit) : "—"}<span class="subln">${d ? `${fq(d.units)} 件/天 · ${esc(BIND_WORD[d.binding] || d.binding)}` : ""}</span></td>
      <td class="${cap > 0 && c.upfront_capital > cap ? "neg" : ""}">${silver(c.upfront_capital)}</td>
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
        <th title="(开专注的利润 − 不开专注的最好利润)÷ 每件专注。绿色 = 超过 premium 门槛">银/专注点</th>
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
function craftStepsHTML(c, res) {
  const steps = c.steps || [];
  const buys = steps.filter(s => s.op === "buy" || s.op === "bid");
  const trans = steps.filter(s => s.op === "transmute");
  const refs = steps.filter(s => s.op === "refine");
  const sell = steps.find(s => s.op === "sell");
  const sum = xs => xs.reduce((a, s) => a + (s.cost || 0), 0);
  const out = [];
  if (buys.length) {
    out.push(`<h4>① 买料 <em>${buys.length} 项,合计 ${num(sum(buys))} 银</em></h4>
      <table class="tight"><thead><tr><th class="l">物品</th><th class="l">城市</th><th>件数</th><th>价位</th><th>金额</th>
        <th class="l">观测</th><th class="l">用途</th></tr></thead><tbody>${buys.map(s => {
      const lv = s.levels || [];
      const price = s.price_from === s.price_to || !s.price_to ? num(s.price_from) : `${num(s.price_from)} ~ ${num(s.price_to)}`;
      const lvTitle = lv.map(l => `${num(l.price)} × ${fq(l.qty)}`).join("\n");
      return `<tr><td class="l">${s.op === "bid" ? `<span class="wtag" title="挂买单:按买一 + 1 挂、多付创建费,收不收得到不知道">挂买单</span>` : ""}${cItem(s.item)}</td>
        <td class="l">${cityDot(s.city)}</td><td>${fq(s.qty)}</td>
        <td title="${esc(lvTitle)}">${price}${lv.length > 1 ? `<span class="subln">吃 ${lv.length} 档</span>` : ""}</td>
        <td>${num(s.cost)}${s.transport ? `<span class="subln">含运费 ${num(s.transport)}</span>` : ""}</td>
        <td class="l">${ageTag(s.age_h)} <span class="sub">${esc(SRC_NAME[s.source] || s.source || "")}${s.seen_at ? " · " + hms(s.seen_at) : ""}</span></td>
        <td class="l">${s.for ? `转化成 ${cItem(s.for, { noTag: true })}` : ""}</td></tr>`;
    }).join("")}</tbody></table>`);
  }
  if (trans.length) {
    out.push(`<h4>② 转化 <em>${trans.length} 步,合计 ${num(sum(trans))} 银 · 在 ${esc(res.city)} 的站里做,不用专注、没有返还</em></h4>
      <table class="tight"><thead><tr><th class="l">从 → 到</th><th>次数</th><th>每次实付</th><th>合计</th></tr></thead><tbody>${trans.map(s =>
      `<tr><td class="l">${cItem(s.from, { noTag: true })} → ${cItem(s.to, { noTag: true })}</td><td>${fq(s.qty)}</td>
        <td>${num(s.unit_fee)}</td><td>${num(s.cost)}</td></tr>`).join("")}</tbody></table>`);
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
  if (c.leftover?.length) {
    const lvWord = { carry: "结转下一批", bid: "砸买单", zero: "按 0" };
    out.push(`<h4>批末余料 <em>一批做完总会剩这些,和件数无关;取得成本合计 ${num(c.leftover.reduce((a, l) => a + (l.cost || 0), 0))}</em></h4>
      <table class="tight"><thead><tr><th class="l">物品</th><th>件数</th><th>取得成本</th><th>估值</th><th class="l">方式</th><th>损失</th></tr></thead><tbody>${
      c.leftover.map(l => `<tr><td class="l">${cItem(l.item)}</td><td>${fq(l.qty)}</td><td>${num(l.cost)}</td><td>${num(l.value)}</td>
        <td class="l">${esc(lvWord[l.mode] || l.mode)}</td><td class="${l.loss > 0 ? "neg" : ""}">${num(l.loss)}</td></tr>`).join("")}</tbody></table>`);
  }
  out.push(warnList(c.warnings));
  return `<div class="detailbox c-steps">${out.join("")}
    <p class="sub">件数是稳态均摊:按回收率的期望返还算,所以带小数;进游戏按整数下单,多出来的零头就是下一批的余料。</p></div>`;
}

$("c-result").addEventListener("click", e => {
  const openCal = e.target.closest("[data-open-cal]");
  if (openCal) {
    $("c-cal").open = true;
    $("c-cal").scrollIntoView({ block: "start", behavior: "smooth" });
    return;
  }
  const st = e.target.closest("[data-open-steps]");
  if (st) {
    craft.open.add(st.dataset.openSteps);
    craftRender(craft.data);
    $("c-result").querySelector(`tr.main[data-cid="${CSS.escape(st.dataset.openSteps)}"]`)?.scrollIntoView({ block: "start", behavior: "smooth" });
    return;
  }
  if (e.target.closest("a")) return;   // 物品名是跳查价页的链接,不当成点行
  const tr = e.target.closest("tr.main[data-cid]");
  if (!tr) return;
  const id = tr.dataset.cid;
  if (craft.open.has(id)) craft.open.delete(id); else craft.open.add(id);
  craftRender(craft.data);
});

function craftHeadHTML(res) {
  const id = res.target, r = refineRole(id), L = craftLine(res.line) || cLine();
  const s = res.sell || {}, v = res.volume || {};
  const spec = res.city === L.city ? `${esc(res.city)}(${L.line === "ORE" ? "矿" : L.line === "HIDE" ? "皮" : L.line === "FIBER" ? "纤维" : "木"}的专精城,回收率 +40)`
    : `${esc(res.city)} <span class="warnish" title="不是这条线的专精城:少 40 点生产加成,回收率明显低">(不是专精城,专精城是 ${esc(L.city)})</span>`;
  const post = s.post
    ? `<div><b>${num(s.post.best_ask)}</b><i>卖一 · 挂卖到手 ${num(s.post.net)} · ${ageTag(s.post.age_h)}</i></div>`
    : `<div><b>—</b><i>${esc(s.city || "")} 没有卖单</i></div>`;
  const take = s.take
    ? `<div><b>${num(s.take.best_bid)}</b><i>买一 · 吃买单${s.take.max_qty != null ? `断崖前能出 ${fq(s.take.max_qty)} 件` : "(深度未知)"} · ${ageTag(s.take.age_h)}</i></div>`
    : `<div><b>—</b><i>${esc(s.city || "")} 没有买单</i></div>`;
  const vol = v.known
    ? `<div><b>${fq(v.per_day)}</b><i>日均成交(${num(v.days)} 天里 ${num(v.days_with_data)} 天有数据)</i></div>`
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
  if (b.aodp_newer?.length) line("抓包之后 AODP 报了不同的价(阶梯可能变了):", b.aodp_newer,
    x => `${esc(cLabel(x.item) || x.item)} @ ${esc(x.city)} ${x.side === "sell" ? "卖单" : "买单"} 抓包 ${num(x.capture_price)}(${fq(x.capture_age_h)}h)→ AODP ${num(x.aodp_price)}(${fq(x.aodp_age_h)}h)`);
  if (b.aodp_ok === false) lists.push(`<li><b>AODP 当前价这次没取到:</b>${esc(b.aodp_error || "")}</li>`);
  return `<details class="c-fold c-data"${lists.length ? " open" : ""}>
    <summary>数据 <em>${parts.join(" · ")}</em></summary>
    ${lists.length ? `<ul class="c-list">${lists.join("")}</ul>` : ""}
    ${warnList(res.warnings)}
    <p class="sub">配方来源 ${esc(res.recipe_source || "—")} · 抓包优先,抓包没有的盘口用 AODP 兜底(只有价、没有件数)。
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
$("c-reset").addEventListener("click", () => {
  if (!confirm("把高级参数全部恢复成服务端默认?(专精、k、站费、生产加成、跨城来源……都会清掉)")) return;
  const p = craft.p;
  Object.assign(p, {
    fce: {}, unlocked: {}, station: {}, tstation: {}, k: null, kAt: "", kSrc: "", kGold: null, bonus: {},
    island: false, sellCity: "", srcCities: [], transport: null, bid: false, bidMax: null, leftover: "",
    maxAge: null, capital: null, share: null, gold: null,
  });
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
    case "c-station": r = cNum(el, 0, 1000); if (r.ok) { if (r.v == null) delete p.station[city]; else p.station[city] = r.v; } break;
    case "c-tstation": r = cNum(el, 0, 1000); if (r.ok) { if (r.v == null) delete p.tstation[city]; else p.tstation[city] = r.v; } break;
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
        if (Number(el.value) === 8) delete p.unlocked[p.line];
        else p.unlocked[p.line] = Number(el.value);
        break;
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
  parts.push(set.length ? `专精 ${set.map(t => `T${t} ${num(fce[t])}`).join("、")}` : "专精按 0 算");
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
  } else if (b.id === "cal-f-apply" && craftCal.fce != null) {
    p.fce[p.line] = { ...cFCE(), [craftCal.fTier]: craftCal.fce };
  } else if (b.id === "cal-l-apply" && craftCal.fces) {
    const f = {};
    CRAFT_TIERS.forEach((t, i) => { f[t] = craftCal.fces[i]; });
    p.fce[p.line] = f;
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
  // 不在这页时退订了实时:回来时手上那份可能旧了,隔得久就强制重算一次
  craftLoad({ force: !!craft.data && Date.now() - craft.loadedAt > 15000 });
}
function craftHide() {
  clearTimeout(craft.timer);
}
viewHooks.craft = {
  hash: () => "craft/" + encodeURIComponent(cTarget()),
  route: craftRoute, show: craftShow, hide: craftHide,
};
