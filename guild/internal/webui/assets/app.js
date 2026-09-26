// 交易终端前端。六个视图共用一份扫描结果,切换只换显示。
"use strict";

const CITY_VAR = {
  "Thetford": "--thetford", "Fort Sterling": "--fort-sterling", "Lymhurst": "--lymhurst",
  "Martlock": "--martlock", "Bridgewatch": "--bridgewatch", "Caerleon": "--caerleon",
  "Brecilien": "--brecilien", "Black Market": "--black-market",
};
// 游戏里的五档品质名。以前写成了 优秀/杰出/卓越/大师,整体错了一位
const QUALITY = ["", "普通", "良好", "优秀", "杰出", "不凡"];
const CONFIDENCE = { high: "高", medium: "中", low: "低" };
// 拼进 class 属性的置信度必须是这三个之一,后端给了别的就按 low 画
const confClass = c => (c === "high" || c === "medium" || c === "low" ? c : "low");
const TREND = { up: "涨", down: "跌", choppy: "震荡", flat: "平" };
// screen 的全部 17 个拒绝原因,文案照抄旧版 report.py 的 REJECT_LABELS。
// 以前缺 future_timestamp / wide_spread / no_bid_side / thin_book,页面上直接露英文 key
const REJECT = {
  one_sided: "单边数据", no_timestamp: "无时间戳", future_timestamp: "数据龄为负(时钟问题)",
  stale: "数据过期", no_history: "无成交历史", stale_history: "历史过期",
  no_baseline: "无基准均价", thin_history: "历史样本不足", deviation: "偏离均价(troll)",
  low_volume: "流水不足", crossed_book: "交叉盘(快照不同步)",
  no_bid_side: "没人在收货(挂买单收不到)", wide_spread: "价差过宽(两侧脱节)",
  thin_book: "挂单太薄(假价)", unprofitable: "税后不赚钱",
  implausible_margin: "利润率不真实(troll)", too_thin: "可吃量 < 1 件",
};
const rejectLabel = k => REJECT[k] || "未归类的原因";
const MODE_LABEL = {
  "taker-taker": "秒买秒卖", "taker-maker": "秒买挂卖",
  "maker-taker": "挂买秒卖", "maker-maker": "挂买挂卖",
};
// 真实盈亏平衡价差(econ.Mode.Breakeven)。服务端 modes 里带 breakeven 时用它的,
// 这里只是老服务端(跨城 modes 还没有这个字段)的兜底
const BREAKEVEN = { "taker-taker": 0.0417, "taker-maker": 0.0695, "maker-taker": 0.0677, "maker-maker": 0.0963 };
// 价差 = 卖价 / 买价 − 1,和盈亏平衡同一个口径。毛利(margin)是税后净利 ÷ 成本,
// 大于 0 就不亏 —— 两个数挨着放、口径不同,以前会把赚钱的单读成低于盈亏平衡
const spreadOf = (buy, sell) => (buy > 0 && sell > 0 ? sell / buy - 1 : null);
// 深度闸门拦下某个执行方式的原因
const BLOCKED = {
  no_bid_side: "买方深度不够,挂买单多半收不到货",
  thin_book: "挂单那一侧太薄,最优价多半是假价",
};
// 和服务端 config.yaml 的 freshness 段一致。扫描结果里没带这两个数
const FRESH_HI = 2, FRESH_MAX = 6;
const MARKET_TAX = 0.04;
// 物品 id → 显示名。扫描结果、查价、物品列表见到一个记一个,记账页和实时页用
const NAMES = new Map();
const noteName = (id, name) => { if (id && name && name !== id) NAMES.set(id, name); };

const $ = id => document.getElementById(id);
// 字段缺失(老服务端、omitempty)时显示 —,不让 NaN / undefined 漏到界面上
const num = n => (n == null || !isFinite(n) ? "—" : Math.round(n).toLocaleString("zh-CN"));
const pct = (n, d = 1) => (n == null || !isFinite(n) ? "—" : (n * 100).toFixed(d) + "%");
const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const cityDot = c => `<span class="city"><i class="cdot" style="--c:var(${CITY_VAR[c] || "--ink-soft"})"></i>${esc(c)}</span>`;
const itemCell = (id, name) =>
  `<span class="item"><img src="/api/icon/${encodeURIComponent(id)}" alt="" loading="lazy"
     onerror="this.style.visibility='hidden'"><a href="#lookup/${encodeURIComponent(id)}">${esc(name)}</a></span>`;

async function getJSON(url, opts) {
  const resp = await fetch(url, opts);
  if (!resp.ok) {
    const body = (await resp.text()).trim();
    // 服务端的错误体是 JSON,直接当文本显示会把一串大括号糊到界面上
    let msg = body;
    try { msg = JSON.parse(body).error || body; } catch (e) { /* 不是 JSON 就照原样 */ }
    const err = new Error(msg || resp.statusText);
    err.status = resp.status;   // 调用方要靠它区分"没这个接口"和"网络抖了一下"
    throw err;
  }
  return resp.json();
}

// ── 视图切换 ───────────────────────────────────────────────
const views = ["desk", "ideas", "lookup", "rank", "book", "live"];
let currentView = null;
function show(name, push = true) {
  if (!views.includes(name)) name = "desk";
  for (const v of views) $("v-" + v).hidden = v !== name;
  for (const b of document.querySelectorAll("nav button")) {
    if (b.dataset.view === name) b.setAttribute("aria-current", "page");
    else b.removeAttribute("aria-current");
  }
  if (push) {
    // 切回查价页时带上刚才看的那件,别让人重新找
    const want = name === "lookup" && lookup.itemId
      ? "lookup/" + encodeURIComponent(lookup.itemId) : name;
    if (location.hash.slice(1) !== want) location.hash = want;
  }
  if (currentView !== name) {
    currentView = name;
    window.scrollTo(0, 0);   // 从机会表下面点物品名跳过来时,别停在半页
  }
  if (name === "lookup") {
    queueFit();   // 隐藏时量不出尺寸,切回来补一次
    // 不在这一页的时候推送来过:切回来补拉一次
    if (lookup.stale) refreshLookupLive();
  }
  if (name === "rank") loadRank();
  if (name === "book") loadBook();
  // 不在这页时只记了数据,没画;顺便对一次账
  if (name === "live") { liveUI.full = true; liveFlush(); liveResyncNow(); }
}
document.querySelector("nav").addEventListener("click", e => {
  const btn = e.target.closest("button[data-view]");
  if (btn) show(btn.dataset.view);
});
window.addEventListener("hashchange", route);
function route() {
  const [view, arg] = location.hash.slice(1).split("/");
  show(view || "desk", false);
  if (view === "lookup" && arg) {
    let id = "";
    try { id = decodeURIComponent(arg); } catch (e) { return; }
    // selectItem 自己会改 hash,再触发一次 hashchange 到这里:那时请求还在飞,跳过,
    // 否则每点一次物品都要请求两遍。已经有一份新鲜数据也跳过。
    // 但上一次失败了、或者那份已经是一分钟前的,就重查 —— 只看 id 相同的话,
    // 查失败之后从别的页点同一件、地址栏回车,都再也不会重试
    const reuse = id === lookup.itemId &&
      (lookup.pending || (lookup.data && Date.now() - lookup.loadedAt < LOOKUP_REUSE_MS));
    if (id && !reuse) selectItem(id);
  }
}

// ── 点表头排序 ─────────────────────────────────────────────
// 排序状态存在 state 里,不只存在 th 的 data-dir 上:数据自己重拉(全量扫描后的销量榜)
// 时要照它重排。以前只有表头箭头记得,重画按服务端原始顺序,箭头和行对不上
function sortable(scope, state, rows, render) {
  document.querySelector(scope).addEventListener("click", e => {
    const th = e.target.closest("th.s");
    if (!th) return;
    const dir = th.dataset.dir === "desc" ? "asc" : "desc";
    for (const other of th.closest("tr").querySelectorAll("th.s")) delete other.dataset.dir;
    th.dataset.dir = dir;
    state.key = th.dataset.k;
    state.dir = dir;
    render(sortBy(rows(), state.key, state.dir));
  });
}
function sortBy(list, key, dir) {
  return [...list].sort((a, b) => {
    const x = a[key] ?? 0, y = b[key] ?? 0;
    const cmp = typeof x === "string" ? x.localeCompare(y, "zh-CN") : (x - y);
    return dir === "desc" ? -cmp : cmp;
  });
}

// ── 扫描结果:总览和机会页共用 ───────────────────────────────
let scan = null;
let ideas = [];      // 同城 + 跨城合成一个池子
let kindFilter = "all";
// 结果里的各种 *_age_hours 是相对"这份结果算出来那一刻"的。之后本地每过一分钟,
// 数据就老一分钟 —— 显示时加上这段流逝(scan.Digest 的约定:摘要不含数据龄,
// 界面自己拿 evaluated_at 加本地流逝算)
let scanBaseAt = 0;
const sinceScan = () => (scanBaseAt ? Math.max(0, (Date.now() - scanBaseAt) / 3600000) : 0);
const ageNow = h => (h == null || !isFinite(h) ? null : h + sinceScan());
// 排序状态单独存:新结果到了、换了筛选,都从原始池子按它重排,不依赖 ideas 原来的顺序
const ideasSort = { key: "daily_profit", dir: "desc" };
// 展开的那一行按行 key 记,结果刷新重画之后照 key 插回去
let ideasOpen = "";
let rejectPick = "";          // 被过滤候选里点开明细的那个原因
let rejectsTouched = false;   // 用户自己开合过「被过滤的候选」就不再替他开合
let rejectsAuto = false;      // 代码最后一次给它设的 open 值(和 HTML 初始的收起一致)
// 明细的品质 / 来源筛选,和列到第几条。换原因时筛选保留(想看的往往就是"良好以上"那几档),
// 列的条数回到一页
const REJECT_PAGE = 300;
const rejectView = { q: 0, src: "", limit: REJECT_PAGE };

const BOTTLENECK = { capital: "本金", source: "产地量", dest: "销地量", depth: "盘口深度" };
const SRC_WORD = { capture: "抓包", aodp: "AODP" };
const srcWord = s => SRC_WORD[s] || (s ? esc(s) : "—");

// 执行方式的两条腿各依托订单簿的哪一边:秒买吃卖单簿(ask)、挂买排在买单簿(bid);
// 秒卖吃买单簿(bid)、挂卖排在卖单簿(ask)。跨城时买腿在产地、卖腿在销地。
// 和服务端 screen.LegSides 同一套映射
function legSides(mode, sides, cross) {
  const [b, s] = String(mode || "").split("-");
  const n = cross ? { fa: "from_ask", fb: "from_bid", ta: "to_ask", tb: "to_bid" }
    : { fa: "ask", fb: "bid", ta: "ask", tb: "bid" };
  const buy = b === "taker" ? n.fa : n.fb, sell = s === "taker" ? n.tb : n.ta;
  return { buy: { name: buy, side: sides[buy] || null }, sell: { name: sell, side: sides[sell] || null } };
}

// 两种玩法字段不同,这里抹平成同一个形状——它们竞争的是同一笔钱,
// 分开排名看不出谁更值得投。新字段(来源、深度、落选报价……)一律原样带着,
// 老服务端没有这些字段时是 undefined,渲染那边缺了就不画
function unify(res) {
  const out = [];
  for (const o of res.opportunities || []) {
    const sides = { ask: o.ask || null, bid: o.bid || null };
    const legs = legSides(o.mode, sides, false);
    out.push(finishIdea({
      kind: "flip", key: `flip|${o.item_id}|${o.quality}|${o.city}`,
      item_id: o.item_id, item_name: o.item_name || o.item_id, quality: o.quality || 1,
      from_city: o.city, to_city: o.city,
      mode: o.mode, mode_label: o.mode_label,
      buy_price: o.my_bid, sell_price: o.my_ask,
      market_bid: o.buy_price, market_ask: o.sell_price,
      cost_per_unit: o.cost_per_unit, profit_per_unit: o.profit_per_unit, margin: o.margin,
      qty: o.qty, capital_used: o.capital_used, daily_profit: o.daily_profit,
      capital_roi: o.capital_roi, absorbable_qty: o.absorbable_qty,
      daily_volume_silver: o.daily_volume_silver, daily_volume_qty: o.daily_volume_qty,
      avg_7d: o.avg_price_7d, avg_30d: o.avg_price_30d,
      z_score: o.has_z ? o.z_score : null, volatility: o.volatility,
      max_age_hours: o.data_age_hours, confidence: o.confidence,
      warnings: o.warnings || [], hints: o.hints || [], modes: o.modes || [],
      bottleneck: null, trips: o.turns_per_day, hours: o.hours_per_turn,
      sides: [["ask", "卖单簿", sides.ask], ["bid", "买单簿", sides.bid]],
      buyLeg: legs.buy, sellLeg: legs.sell,
      buy_leg_source: o.buy_leg_source, sell_leg_source: o.sell_leg_source,
      buy_leg_age_hours: o.buy_leg_age_hours, sell_leg_age_hours: o.sell_leg_age_hours,
      depth_checked: o.depth_checked,
      fill_position: o.has_fill_position ? o.fill_position : null,
      risk_tags: o.risk_tags || [],
    }));
  }
  for (const r of res.routes || []) {
    const sides = {
      from_ask: r.from_ask || null, from_bid: r.from_bid || null,
      to_ask: r.to_ask || null, to_bid: r.to_bid || null,
    };
    const legs = legSides(r.mode, sides, true);
    out.push(finishIdea({
      kind: "arb", key: `arb|${r.item_id}|${r.quality}|${r.from_city}|${r.to_city}`,
      item_id: r.item_id, item_name: r.item_name || r.item_id, quality: r.quality || 1,
      from_city: r.from_city, to_city: r.to_city,
      mode: r.mode, mode_label: r.mode_label,
      buy_price: r.buy_price, sell_price: r.sell_price,
      cost_per_unit: r.cost_per_unit, profit_per_unit: r.profit_per_unit, margin: r.margin,
      qty: r.qty, capital_used: r.capital_used, daily_profit: r.daily_profit,
      capital_roi: r.capital_roi, trip_profit: r.trip_profit,
      daily_volume_silver: null,   // 跨城没有流水,只有两端各自的日均件数
      source_daily_qty: r.source_daily_qty, dest_daily_qty: r.dest_daily_qty,
      buy_depth_qty: r.buy_depth_qty, sell_depth_qty: r.sell_depth_qty,
      travel_hours: r.travel_hours,
      z_score: r.has_z ? r.z_score : null, volatility: r.volatility,
      max_age_hours: r.max_age_hours, confidence: r.confidence,
      warnings: r.warnings || [], hints: [], modes: r.modes || [],
      bottleneck: r.bottleneck || null, trips: r.trips_per_day, hours: r.hours_per_trip,
      sides: [["from_ask", "产地卖单簿", sides.from_ask], ["from_bid", "产地买单簿", sides.from_bid],
        ["to_ask", "销地卖单簿", sides.to_ask], ["to_bid", "销地买单簿", sides.to_bid]],
      buyLeg: legs.buy, sellLeg: legs.sell,
      buy_leg_source: r.buy_leg_source, sell_leg_source: r.sell_leg_source,
      buy_leg_age_hours: r.buy_leg_age_hours, sell_leg_age_hours: r.sell_leg_age_hours,
      depth_checked: r.depth_checked,
      fill_position: null,
      risk_tags: r.risk_tags || [],
    }));
  }
  return out;
}

function finishIdea(r) {
  // 两条腿各用了谁的价。老服务端没有 *_leg_source,退回看腿依托的那一边的 source
  r.buy_src = r.buy_leg_source ?? r.buyLeg.side?.source ?? null;
  r.sell_src = r.sell_leg_source ?? r.sellLeg.side?.source ?? null;
  r.capture_legs = [r.buy_src, r.sell_src].filter(s => s === "capture").length;
  // 「盘口」列排序用:两条腿里薄的那条的近价件数。没有抓包深度的腿不参与,两条都没有记 -1
  const near = [r.buyLeg.side?.depth?.qty_near, r.sellLeg.side?.depth?.qty_near].filter(v => v != null);
  r.leg_qty_near = near.length ? Math.min(...near) : -1;
  r.mode_text = r.mode_label || MODE_LABEL[r.mode] || "—";
  return r;
}

// 表头就是这张表。th 和 td 都从这里生成,不会再有 14 个表头对 15 个格子的错位
const IDEA_COLS = [
  { k: "item_name", label: "物品", l: true },
  { k: "quality", label: "品质", l: true },
  { k: "from_city", label: "路线", l: true },
  { k: "mode_text", label: "执行", l: true },
  { k: "buy_price", label: "买 → 卖", tip: "照这两个价进游戏下单。同城是选中执行方式下我挂/吃的价;跨城吃单腿沿阶梯走过的,是走到的最差那一档。下面一行是价差(卖价 / 买价 − 1),拿它和盈亏平衡比" },
  { k: "margin", label: "毛利", tip: "税后净利 ÷ 成本(成本含创建费),大于 0 就不亏。它和盈亏平衡不是一个口径,别拿来比 —— 盈亏平衡比的是左边那列的价差。下面一行是单件净赚" },
  { k: "daily_volume_silver", label: "日流水", cls: "hide-md", tip: "7 日日均成交件数 × 均价。跨城没有这一项" },
  { k: "qty", label: "可吃量", tip: "一轮做多少件:日成交量 × absorb_ratio、盘口深度、本金三者取小。下面一行是瓶颈" },
  // 窄屏下收起:日收益下面那行的本金 % 和展开行里都有
  { k: "capital_used", label: "占用本金", cls: "hide-sm" },
  { k: "trips", label: "轮/天", cls: "hide-md", tip: "挂单腿要等成交,一天转不了几轮" },
  { k: "daily_profit", label: "日收益", tip: "排序主键。下面一行是占总本金的比例" },
  { k: "z_score", label: "z", cls: "hide-md", tip: "现价相对 30 日常态的位置。>1.5σ 说明现在偏贵,这个价差可能只是一时的" },
  { k: "volatility", label: "波动", cls: "hide-md", tip: "30 日价格变异系数" },
  { k: "leg_qty_near", label: "盘口", tip: "买腿 / 卖腿依托的那一边,最优价附近的挂单件数。只有抓包有件数,AODP 不给" },
  { k: "max_age_hours", label: "数据龄", tip: "两侧里较旧的那个。圆点是置信度,悬停看原因" },
];
const STRING_KEYS = new Set(["item_name", "from_city", "mode_text"]);

function ideasFiltered() {
  const conf = $("i-conf").value, city = $("i-city").value, src = $("i-src").value;
  const maxAge = Number($("i-age").value) || 0;
  const rank = { low: 0, medium: 1, high: 2 };
  return ideas.filter(r =>
    (kindFilter === "all" || r.kind === kindFilter) &&
    (!conf || (rank[r.confidence] ?? 0) >= rank[conf]) &&
    (!city || r.from_city === city || r.to_city === city) &&
    (!src || (src === "capture" ? r.capture_legs > 0 : r.capture_legs === 0)) &&
    (!maxAge || (ageNow(r.max_age_hours) ?? 0) <= maxAge));
}

function renderIdeasHead() {
  $("i-head").innerHTML = IDEA_COLS.map(c => {
    const on = c.k === ideasSort.key;
    const cls = [c.l ? "l" : "", c.cls || ""].filter(Boolean).join(" ");
    return `<th data-k="${c.k}"${cls ? ` class="${cls}"` : ""}${c.tip ? ` title="${esc(c.tip)}"` : ""}${
      on ? ` aria-sort="${ideasSort.dir === "desc" ? "descending" : "ascending"}"` : ""}>${c.label}${
      on ? `<span class="arrow">${ideasSort.dir === "desc" ? "▼" : "▲"}</span>` : ""}</th>`;
  }).join("");
}
$("i-head").addEventListener("click", e => {
  const th = e.target.closest("th[data-k]");
  if (!th) return;
  const k = th.dataset.k;
  if (ideasSort.key === k) ideasSort.dir = ideasSort.dir === "desc" ? "asc" : "desc";
  else { ideasSort.key = k; ideasSort.dir = STRING_KEYS.has(k) ? "asc" : "desc"; }
  renderIdeas();
});

// 来源徽标:两条腿的价都是成员抓包抓到的标「抓」,只有一条是标「半」
function srcTag(r) {
  if (r.buy_src == null && r.sell_src == null) return "";
  if (r.capture_legs === 2) return `<i class="src" title="两条腿的价都是成员抓包抓到的,带挂单件数">抓</i>`;
  if (r.capture_legs === 1) return `<i class="src half" title="一条腿的价来自抓包,另一条来自 AODP">半</i>`;
  return "";
}

const qualityTag = q => `<span class="q" style="--q:var(--q${Number(q) || 1})">${QUALITY[q] || "品质 " + Number(q)}</span>`;
const mistsTag = r => (r.risk_tags || []).includes("mists")
  ? `<span class="tag risk" title="一端是 Brecilien,要穿迷雾,有被劫风险。收益里没折这一项,置信度封顶 medium${
    r.travel_hours ? `;路上按单程 ${r.travel_hours.toFixed(1)} 小时估,没实测` : ""}">经迷雾</span>` : "";

// 数据龄悬停说明。按 confidence 字段说原因,不按 warnings 猜:
// 同城的 z>1.5σ、波动大也会写进 warnings,但不降级;跨城每条都带一句瓶颈说明
function confidenceReason(r) {
  const age = ageNow(r.max_age_hours) ?? 0;
  const mists = (r.risk_tags || []).includes("mists");
  if (r.kind === "arb") {
    if (r.confidence === "low") {
      const why = [];
      if (r.max_age_hours > 4) why.push("两端有一侧数据超过 4 小时");
      if (r.z_score != null && r.z_score > 1.5) why.push(`销地现价高于 30 日常态 ${r.z_score.toFixed(1)}σ`);
      return `low — ${why.join(";") || "见行下的提示"}`;
    }
    if (r.confidence === "high") return `high — 两端的价都在 ${FRESH_HI} 小时内,销地价格平稳`;
    const why = [];
    if (r.max_age_hours > FRESH_HI) why.push(`有一侧超过 ${FRESH_HI} 小时`);
    if (r.volatility > 0.25) why.push(`销地价格波动大(变异系数 ${r.volatility.toFixed(2)})`);
    if (mists) why.push("经迷雾(一端是 Brecilien),置信度封顶 medium");
    return `medium — ${why.join(";") || `数据在 4 小时内`}`;
  }
  if (r.confidence === "low")
    return `low — 贴着过滤阈值边缘${r.warnings.length ? ":" + r.warnings.join(";") : ""}`;
  if (r.confidence === "high") return `high — 买卖两侧都在 ${FRESH_HI} 小时内`;
  return `medium — 买卖两侧都在 ${FRESH_MAX} 小时内,但超过 ${FRESH_HI} 小时` +
    (age > r.max_age_hours + 0.05 ? `(现在是 ${age.toFixed(1)} 小时,算这份结果时是 ${r.max_age_hours.toFixed(1)})` : "");
}

// 查价格子一侧的 depth / note:和机会页卡片同一边的 depth / note 是同一份(服务端
// scan.MergeSide)。选中抓包、最优档在可信窗口内时有 depth;太旧时 depth 为空、note 说明
// "未参与判定";选了 AODP 时两个都空。查价格子和右栏共用这句
function gateText(s, depthH) {
  const d = s?.depth;
  if (!d) return s?.note || "";
  return `最优档 ${num(d.qty_at_best)} 件 · 最优价 ${pct(d.near_pct, 0)} 以内 ${num(d.qty_near)} 件(${Number(d.levels_near)} 档)` +
    (d.gap_after_near > 0 ? ` · 近价窗口外下一档差 ${pct(d.gap_after_near, 0)}${d.gap_after_near >= 0.2 ? ",断崖" : ""}` : "") +
    (d.truncated ? " · 阶梯截断,近价件数只是下限" : "") +
    `。只算 ${depthH} 小时内看到的档,和机会页卡片同一份判定`;
}

// 一条腿依托的那一边盘口的近价件数。只有抓包、而且在深度可信窗口内才有
function legDepth(leg, word) {
  const s = leg.side;
  if (!s) return { txt: "?", tip: `${word}:这一边没有价` };
  const d = s.depth;
  if (!d) {
    const why = s.source === "capture" ? (s.note || "抓包的深度不在可信窗口内") : "价来自 AODP,不给件数";
    return { txt: "?", tip: `${word}:${why}` };
  }
  const cliff = d.gap_after_near >= 0.2;
  const tip = `${word}:抓包 · 最优档 ${num(d.qty_at_best)} 件 · 最优价 ${pct(d.near_pct, 0)} 以内 ${num(d.qty_near)} 件(${Number(d.levels_near)} 档)` +
    (d.gap_after_near > 0 ? ` · 再往下一档差 ${pct(d.gap_after_near, 0)}${cliff ? ",断崖" : ""}` : "") +
    (d.truncated ? " · 阶梯截断,件数只是下限" : "");
  return { txt: `<span class="${cliff ? "cliff" : ""}">${num(d.qty_near)}${d.truncated ? "+" : ""}</span>`, tip };
}

function bookCell(r) {
  const any = r.sides.some(([, , s]) => s);
  if (!any && r.depth_checked == null)
    return `<span class="nodata" title="AODP 不返回挂单数量。成员开着客户端在游戏里翻市场,抓包才有件数">—</span>`;
  const b = legDepth(r.buyLeg, "买腿"), s = legDepth(r.sellLeg, "卖腿");
  const unchecked = r.depth_checked === false
    ? `<span class="subln"><span class="tag medium" title="两条腿依托的那一边不都有可信的抓包深度。不是深度不够,是不知道 —— 回游戏里点进物品详情页翻一眼">未核深度</span></span>` : "";
  return `<span class="legtxt" title="${esc(b.tip + "\n" + s.tip)}">${b.txt}<i>/</i>${s.txt}</span>${unchecked}`;
}

function routeCell(r) {
  const cities = r.kind === "arb"
    ? `<span class="route">${cityMark(r.from_city)}<span>→</span>${cityMark(r.to_city)}</span>`
    : cityMark(r.from_city);
  return `<div class="routecell">${cities}<span class="tags"><span class="kind ${r.kind === "arb" ? "arb" : "flip"}">${
    r.kind === "arb" ? "跨城" : "同城"}</span>${mistsTag(r)}</span></div>`;
}

function ideaRowHTML(r) {
  const age = ageNow(r.max_age_hours);
  const fade = Math.min(1, (age ?? 0) / Math.max(FRESH_MAX, 0.5)).toFixed(2);
  const k = esc(r.key);
  const z = r.z_score == null ? "—"
    : `<span class="${r.z_score > 1.5 ? "neg" : r.z_score < -1 ? "pos" : ""}">${r.z_score.toFixed(1)}σ</span>`;
  const spread = spreadOf(r.buy_price, r.sell_price);
  const be = r.modes.find(m => m.mode === r.mode)?.breakeven ?? BREAKEVEN[r.mode];
  const spreadTip = spread != null
    ? `\n价差 ${pct(spread, 2)}(卖价 / 买价 − 1),这个执行方式的盈亏平衡价差是 ${be != null ? pct(be, 2) : "—"}` : "";
  const quoteTip = (r.kind === "flip"
    ? `${r.mode_text}:买 ${num(r.buy_price)}、卖 ${num(r.sell_price)}。市场现在买一 ${num(r.market_bid)} / 卖一 ${num(r.market_ask)}`
    : `${r.mode_text}:在 ${r.from_city} 买 ${num(r.buy_price)},运到 ${r.to_city} 卖 ${num(r.sell_price)}`) + spreadTip;
  const vol = r.daily_volume_silver != null
    ? silver(r.daily_volume_silver)
    : `<span class="nodata" title="跨城没有流水这一项:产地日均 ${num(r.source_daily_qty)} 件、销地日均 ${num(r.dest_daily_qty)} 件">—</span>`;
  const notes = [];
  if (r.warnings.length) notes.push(["warn", r.warnings]);
  if (r.hints.length) notes.push(["hint", r.hints]);
  const cells = [
    `<td class="l"><div class="item"><img src="${iconURL(r.item_id)}" alt="" loading="lazy" onerror="this.style.visibility='hidden'">
      <span style="min-width:0"><span class="item-name"><a href="#lookup/${encodeURIComponent(r.item_id)}" title="查这个物品在所有城市的价格">${esc(r.item_name)}</a>${srcTag(r)}</span>
      <span class="item-id">${esc(r.item_id)}</span></span></div></td>`,
    `<td class="l">${qualityTag(r.quality)}</td>`,
    `<td class="l">${routeCell(r)}</td>`,
    `<td class="l sub">${esc(r.mode_text)}</td>`,
    `<td class="quote" title="${esc(quoteTip)}">${num(r.buy_price)}<i>→</i>${num(r.sell_price)}${
      spread != null ? `<span class="subln">价差 ${pct(spread)}</span>` : ""}</td>`,
    `<td>${pct(r.margin)}<span class="subln ${r.profit_per_unit > 0 ? "pos" : "neg"}">单件 ${num(r.profit_per_unit)}</span></td>`,
    `<td class="hide-md">${vol}</td>`,
    `<td>${num(r.qty)}${r.bottleneck ? `<span class="subln" title="这一项限住了可吃量">${esc(BOTTLENECK[r.bottleneck] || "瓶颈未归类")}</span>` : ""}</td>`,
    `<td class="hide-sm">${silver(r.capital_used)}</td>`,
    `<td class="sub hide-md">${r.trips ? r.trips.toFixed(1) : "—"}</td>`,
    `<td class="pcell"><span class="profit">${silver(r.daily_profit)}</span>${
      r.capital_roi != null ? `<span class="roi">本金 ${pct(r.capital_roi)}</span>` : ""}</td>`,
    `<td class="hide-md">${z}</td>`,
    `<td class="hide-md">${r.volatility ? r.volatility.toFixed(2) : "—"}</td>`,
    `<td>${bookCell(r)}</td>`,
    `<td><div class="agecell" title="${esc(confidenceReason(r))}"><i class="pip ${confClass(r.confidence)}"></i>${ageSpan(r.max_age_hours)}</div>
      <span class="subln">${CONFIDENCE[r.confidence] || "低"}</span></td>`,
  ];
  const main = `<tr class="main open${r.confidence === "low" ? " edge" : ""}${notes.length ? "" : " last"}" data-key="${k}" style="--fade:${fade}">${cells.join("")}</tr>`;
  return main + notes.map(([cls, items], i) =>
    `<tr class="rnote${i === notes.length - 1 ? " last" : ""}" data-for="${k}" style="--fade:${fade}"><td colspan="${IDEA_COLS.length}"><span class="${cls}">${items.map(esc).join(" · ")}</span></td></tr>`).join("");
}

// 会随时间走的数据龄:data-h 是算结果那一刻的小时数,tickAges 定时加上本地流逝重写
const ageSpan = h => (h == null || !isFinite(h) ? "—"
  : `<span class="tick-age" data-h="${Number(h)}">${(h + sinceScan()).toFixed(1)}h</span>`);
function tickAges(root = document) {
  const d = sinceScan();
  for (const el of root.querySelectorAll(".tick-age")) el.textContent = (+el.dataset.h + d).toFixed(1) + "h";
}
setInterval(() => { if (scan) tickAges($("v-ideas")); }, 30000);

function renderIdeas(opts = {}) {
  if (!scan) return;
  renderIdeasHead();
  const rows = sortBy(ideasFiltered(), ideasSort.key, ideasSort.dir);
  const tbody = $("ideas-rows");
  const prev = opts.flash ? tbody._vals : null;
  const y = window.scrollY;
  tbody.innerHTML = rows.map(ideaRowHTML).join("");
  tbody._rows = new Map(rows.map(r => [r.key, r]));
  tbody._vals = new Map(ideas.map(r => [r.key, r.daily_profit]));
  $("ideas-empty").hidden = rows.length > 0;
  renderIdeasCount(rows);
  if (ideasOpen) insertDetail(ideasOpen);   // 被筛掉了就先不插,key 留着,筛回来还是开的
  // 新结果里变了的行闪一下:日收益涨绿跌红,新冒出来的行整个物品格闪
  if (prev && prev.size) {
    for (const r of rows) {
      const tr = rowEl(r.key);
      if (!tr) continue;
      const old = prev.get(r.key);
      if (old === undefined) flash(tr.firstElementChild, "up");
      else if (Math.abs(old - r.daily_profit) >= 1) flash(tr.querySelector("td.pcell"), r.daily_profit > old ? "up" : "down");
    }
  }
  if (currentView === "ideas") window.scrollTo(0, y);
}

function flash(el, dir) {
  if (!el) return;
  el.classList.remove("flash-up", "flash-down");
  void el.offsetWidth;                 // 强制重排,让同一元素能连续播动画
  el.classList.add("flash-" + dir);
}

const rowEl = key => $("ideas-rows").querySelector(`tr.main[data-key="${CSS.escape(key)}"]`);

function renderIdeasCount(rows) {
  const by = { high: 0, medium: 0, low: 0 };
  let lo = Infinity, hi = -Infinity, flip = 0, arb = 0;
  for (const r of rows) {
    by[confClass(r.confidence)]++;
    if (r.kind === "arb") arb++; else flip++;
    const a = ageNow(r.max_age_hours);
    if (a != null) { lo = Math.min(lo, a); hi = Math.max(hi, a); }
  }
  const hidden = ideas.length - rows.length;
  const hiddenTxt = hidden > 0 ? ` · 另有 ${hidden} 条被筛掉` : "";
  const cap = scan.capture;
  const capTxt = cap && cap.enabled !== undefined
    ? ` · <span class="capn" title="融合后卖单簿 ${num(cap.asks_used)} 边、买单簿 ${num(cap.bids_used)} 边用的是抓包价">抓包用了 ${num((cap.asks_used || 0) + (cap.bids_used || 0))} 个价</span>` : "";
  $("i-count").innerHTML = rows.length
    ? `<b>${rows.length}</b> 条机会(同城 ${flip} / 跨城 ${arb})— high ${by.high} / medium ${by.medium} / low ${by.low}` +
      (isFinite(lo) ? ` · 数据龄 ${lo.toFixed(1)}–${hi.toFixed(1)}h` : "") + hiddenTxt + capTxt
    : `<b>0</b> 条机会通过当前筛选${hiddenTxt}${capTxt}`;
}

// ── 展开行:四种执行方式 + 两边盘口 ──
// 最赚的那个往往两头都要等,用户可能宁愿要快的——这个选择不该由代码替他做
const MODE_NOTE = {
  "taker-taker": "立刻成交,不用等。付卖一、收买一,只交 4% 税,没有创建费",
  "taker-maker": "货立刻到手,卖单挂着等。卖单收 2.5% 创建费,下单就扣",
  "maker-taker": "买单挂着等,拿到货立刻脱手。买单收 2.5% 创建费,下单就扣",
  "maker-maker": "价格最好,但两头都要等成交,两边都付创建费",
};

function modesTable(r) {
  if (!r.modes.length) return `<p class="note">这条没有执行方式明细。</p>`;
  return `<table class="tight"><thead><tr>
      <th class="l">执行方式</th><th>买入价</th><th>卖出价</th><th>单件利润</th>
      <th title="税后净利 ÷ 成本(成本含创建费)。大于 0 就不亏;它不是价差,别拿它和盈亏平衡比">毛利率</th>
      <th title="卖出价 / 买入价 − 1,没扣任何税费。拿它和右边的盈亏平衡比">价差</th>
      <th title="真实盈亏平衡价差:左边的价差至少要到这么多才不亏。比几个费率直接相加高">盈亏平衡</th>
      <th>一轮耗时</th><th>轮/天</th><th>日收益</th><th class="l">说明</th>
    </tr></thead><tbody>${r.modes.map(m => {
      const hours = m.hours_per_trip ?? m.hours_per_turn;
      const turns = m.trips_per_day ?? m.turns_per_day;
      const be = m.breakeven ?? BREAKEVEN[m.mode];
      const spread = spreadOf(m.buy_price, m.sell_price);
      const why = m.blocked
        ? `被深度闸门拦下:${esc(BLOCKED[m.blocked] || rejectLabel(m.blocked))}。账照算给你看,不参与挑选`
        : esc(MODE_NOTE[m.mode] || "");
      return `<tr class="${m.blocked ? "blocked" : ""}">
        <td class="l"><b>${esc(m.label || MODE_LABEL[m.mode] || "")}</b>${m.mode === r.mode ? ' <span class="tag high">选用</span>' : ""}${
          m.blocked ? ' <span class="tag low">被拦</span>' : ""}</td>
        <td>${num(m.buy_price)}</td><td>${num(m.sell_price)}</td>
        <td class="${m.profit_per_unit > 0 ? "pos" : "neg"}">${num(m.profit_per_unit)}</td>
        <td>${pct(m.margin)}</td>
        <td class="${spread != null && be != null ? (spread > be ? "pos" : "neg") : ""}">${spread != null ? pct(spread, 2) : "—"}</td>
        <td title="名义摩擦(几个费率相加)${pct(m.friction)}">${be != null ? pct(be, 2) : "—"}</td>
        <td class="sub">${hours ? hours.toFixed(1) + "h" : "—"}</td>
        <td class="sub">${turns ? turns.toFixed(1) : "—"}</td>
        <td class="${m.daily_profit > 0 ? "pos" : "neg"}"><b>${num(m.daily_profit)}</b></td>
        <td class="l sub why">${why}</td>
      </tr>`;
    }).join("")}</tbody></table>`;
}

// 每个盘口边一行:用了谁的价、多旧、深度、落选的另一路
function sidesTable(r) {
  const rows = r.sides.filter(([, , s]) => s);
  if (!rows.length) return "";
  const legWord = name => [r.buyLeg.name === name ? "买腿" : "", r.sellLeg.name === name ? "卖腿" : ""]
    .filter(Boolean).join("、");
  const badge = s => s === "capture" ? `<i class="src" style="margin:0">抓</i> 抓包` : srcWord(s);
  return `<h4>两边盘口 <em>腿用的是哪一边:秒买吃卖单簿、挂买排在买单簿;秒卖吃买单簿、挂卖排在卖单簿</em></h4>
    <table class="tight"><thead><tr>
      <th class="l">盘口边</th><th class="l">用在</th><th class="l">来源</th><th>价格</th><th>数据龄</th>
      <th title="最优价那一档的件数">最优档</th><th title="最优价附近(near_pct 以内)的件数和档数">近价件数</th>
      <th>近价档</th><th title="近价窗口外的下一档比最优价差多少。20% 以上算断崖,最优价没支撑">再下一档</th>
      <th title="这一边抓到的全部件数,含 1 银那种占位单,别拿它当深度">总件数</th>
      <th class="l">落选的另一路</th><th class="l">说明</th>
    </tr></thead><tbody>${rows.map(([name, label, s]) => {
      const d = s.depth;
      const use = legWord(name);
      const notes = [s.note, d?.truncated ? "阶梯截断,近价件数只是下限" : ""].filter(Boolean);
      return `<tr>
        <td class="l">${esc(label)}</td>
        <td class="l">${use ? `<b>${use}</b>` : '<span class="sub">—</span>'}</td>
        <td class="l">${badge(s.source)}</td>
        <td>${s.price ? num(s.price) : "—"}</td>
        <td class="sub">${ageSpan(s.age_hours)}</td>
        <td>${d ? num(d.qty_at_best) : "—"}</td>
        <td>${d ? `${num(d.qty_near)}${d.truncated ? "+" : ""}<span class="sub"> / ${pct(d.near_pct, 0)}</span>` : "—"}</td>
        <td class="sub">${d ? Number(d.levels_near) : "—"}</td>
        <td class="${d && d.gap_after_near >= 0.2 ? "neg" : "sub"}">${d ? (d.gap_after_near > 0 ? pct(d.gap_after_near, 0) : "没有下一档") : "—"}</td>
        <td class="sub">${d ? num(d.qty_total) : "—"}</td>
        <td class="l sub">${s.alt ? `${srcWord(s.alt.source)} ${num(s.alt.price)}(${ageSpan(s.alt.age_hours)} 前)` : ""}</td>
        <td class="l sub why">${notes.map(esc).join(";")}</td>
      </tr>`;
    }).join("")}</tbody></table>`;
}

function detailFacts(r) {
  const f = [];
  const legAge = (w, src, h) => src != null
    ? `${w} <b>${srcWord(src)}</b>${h != null ? ` · ${ageSpan(h)}` : ""}` : "";
  f.push(legAge("买腿", r.buy_src, r.buy_leg_age_hours), legAge("卖腿", r.sell_src, r.sell_leg_age_hours));
  if (r.kind === "flip") {
    f.push(`市场买一 <b>${num(r.market_bid)}</b> / 卖一 <b>${num(r.market_ask)}</b>`,
      `7 日均价 <b>${num(r.avg_7d)}</b> · 30 日 <b>${num(r.avg_30d)}</b>`,
      `日均成交 <b>${num(r.daily_volume_qty)}</b> 件(流水 ${silver(r.daily_volume_silver)})`,
      `敢吃 <b>${num(r.absorbable_qty)}</b> 件/天`);
  } else {
    f.push(`产地日均 <b>${num(r.source_daily_qty)}</b> 件 · 销地日均 <b>${num(r.dest_daily_qty)}</b> 件`);
    if (r.buy_depth_qty) f.push(`买腿按阶梯只吃得到 <b>${num(r.buy_depth_qty)}</b> 件`);
    if (r.sell_depth_qty) f.push(`卖腿按阶梯只卖得掉 <b>${num(r.sell_depth_qty)}</b> 件`);
    if (r.trip_profit != null) f.push(`一趟净赚 <b>${num(r.trip_profit)}</b>`);
    if (r.travel_hours) f.push(`路上单程 <b>${r.travel_hours.toFixed(1)}</b> 小时`);
  }
  f.push(`z <b>${r.z_score == null ? "—" : r.z_score.toFixed(1) + "σ"}</b> · 波动 <b>${r.volatility ? r.volatility.toFixed(2) : "—"}</b>`);
  if (r.depth_checked != null) f.push(r.depth_checked ? "两条腿的深度都核过" : `<span class="tag medium">未核深度</span>`);
  return `<div class="facts2">${f.filter(Boolean).map(x => `<span>${x}</span>`).join("")}</div>`;
}

// 7 日成交均价落在买一和卖一之间的位置。服务端不做钳位,可能 <0 或 >1
function fillPosHTML(r) {
  if (r.fill_position == null || !isFinite(r.fill_position)) return "";
  const p = r.fill_position, at = Math.min(1, Math.max(0, p)) * 100;
  const where = p < 0 ? "比买一还低" : p > 1 ? "比卖一还高" : `落在买一 → 卖一之间的 ${(p * 100).toFixed(0)}% 处`;
  return `<h4>成交位置 <em>基于 AODP 历史均价,仅供参考</em></h4>
    <div class="fillpos"><div class="fp-track" title="左 = 买一,右 = 卖一"><i style="left:${at.toFixed(1)}%"></i></div>
    <span>7 日成交均价${where}。AODP 的成交只统计卖单,均价天生偏向卖价一侧,所以这个数只展示、不参与任何判定。</span></div>`;
}

function detailHTML(r) {
  // 同城 modes 是四种全列;跨城只列可行的(arb.evaluate:两边有价、扣完税费还赚、
  // 利润率不离谱;被深度闸门拦下的也在,灰着),常常只有一两种,标题照实数写
  const n = r.modes.length;
  const title = r.kind === "arb" ? `${n} 种可行的执行方式` : n === 4 ? "四种执行方式" : `${n} 种执行方式`;
  const why = r.kind === "arb"
    ? "跨城只列扣完税费还赚的;选用的是日收益最高的那个,不是单件利润最高的"
    : "选用的是日收益最高的那个,不是单件利润最高的";
  return `<div class="detailbox">
    <h4>${title} <em>${why}</em></h4>
    ${modesTable(r)}
    ${detailFacts(r)}
    ${sidesTable(r)}
    ${fillPosHTML(r)}
    <p class="note">挂单腿要等成交,一天转不了几轮;秒买秒卖单件少,周转快起来总量可能反超。
      ${r.trips ? `一轮 ${(r.hours || 0).toFixed(1)} 小时,一天 ${r.trips.toFixed(1)} 轮 · ` : ""}
      占用本金 ${num(r.capital_used)} · 日收益 ${num(r.daily_profit)}
      <button class="mini" data-log="${esc(r.key)}">记一笔</button></p>
  </div>`;
}

function insertDetail(key) {
  const tbody = $("ideas-rows");
  const r = tbody._rows?.get(key);
  if (!r) return false;
  // 插在这一条的最后一行(主行或它下面的提示行)后面
  const mine = [...tbody.children].filter(tr => tr.dataset.key === key || tr.dataset.for === key);
  const last = mine[mine.length - 1];
  if (!last) return false;
  last.insertAdjacentHTML("afterend",
    `<tr class="detail"><td colspan="${IDEA_COLS.length}">${detailHTML(r)}</td></tr>`);
  return true;
}

$("ideas-rows").addEventListener("click", e => {
  const log = e.target.closest("button[data-log]");
  if (log) { openTrade(log.dataset.log); return; }
  const tr = e.target.closest("tr.main");
  if (!tr || e.target.closest("a") || e.target.closest("button")) return;
  const key = tr.dataset.key;
  for (const d of $("ideas-rows").querySelectorAll("tr.detail")) d.remove();
  if (ideasOpen === key) { ideasOpen = ""; return; }
  ideasOpen = key;
  insertDetail(key);
});

$("kind-seg").addEventListener("click", e => {
  const b = e.target.closest("button[data-kind]");
  if (!b) return;
  kindFilter = b.dataset.kind;
  for (const other of $("kind-seg").children)
    other.setAttribute("aria-pressed", other === b ? "true" : "false");
  renderIdeas();
});
for (const id of ["i-conf", "i-city", "i-src", "i-age"]) $(id).addEventListener("change", () => renderIdeas());

// ── 覆盖率:机会页和总览共用 ──
// 分母是"每城可能的报价点数" = 评估的 (物品, 品质) 组合数 × 2(买卖两侧)。以前总览按
// with_data 当分母,一座城只有 1 个点、1 个新鲜也显示 100%;后来写死成物品数 × 2,
// 扫描并进抓到的别的品质之后又不对了。组合数用服务端的 pairs,老服务端没有就退回物品数
function renderCoverage(res, ids) {
  const cov = res.coverage || [];
  const items = (res.item_ids || []).length;
  const pairs = res.pairs || items;
  let max = pairs * 2;
  for (const c of cov) max = Math.max(max, c.with_data || 0, c.capture_sides || 0);
  const w = n => (max ? Math.max(0, n) / max * 100 : 0).toFixed(2) + "%";
  const hasCap = cov.some(c => c.capture_sides !== undefined);
  $(ids.grid).innerHTML = cov.map(c => {
    const fresh = c.within_2h || 0;
    const usable = Math.max(0, (c.within_threshold || 0) - fresh);
    const aged = Math.max(0, (c.with_data || 0) - (c.within_threshold || 0));
    const none = Math.max(0, max - (c.with_data || 0));
    const med = c.has_median ? c.median_age_hours.toFixed(1) + "h" : "无数据";
    const cap = c.capture_sides !== undefined;
    const used = c.capture_used || 0, sides = c.capture_sides || 0;
    const capMed = c.capture_has_median ? `中位 ${c.capture_median_age_hours.toFixed(1)}h` : "无数据";
    return `<div class="cov-row" style="--c:var(${CITY_VAR[c.city] || "--ink-soft"})">
      <div class="cov-city"><i class="cdot"></i>${esc(cityShort(c.city))}</div>
      <div class="cov-bars">
        <div class="cov-bar" title="${esc(`AODP:${FRESH_HI} 小时内 ${fresh} · ${FRESH_HI}–${FRESH_MAX} 小时 ${usable} · 超过 ${FRESH_MAX} 小时 ${aged} · 无人上传 ${none}(共 ${max} 个点)`)}">
          <i class="fresh" style="width:${w(fresh)}"></i><i class="usable" style="width:${w(usable)}"></i><i class="aged" style="width:${w(aged)}"></i></div>
        ${cap ? `<div class="cov-bar cap" title="${esc(`抓包:${sides} 个盘口边抓到了挂单,其中 ${used} 个的价用上了(其余是 AODP 更新)`)}">
          <i class="capused" style="width:${w(used)}"></i><i class="caponly" style="width:${w(sides - used)}"></i></div>` : ""}
      </div>
      <div class="cov-num"><b>${num(c.within_threshold)}</b>/${num(max)} 可用 · ${med}${
        cap ? `<span>抓包 ${num(used)}/${num(sides)} 用上 · ${capMed}</span>` : ""}</div>
    </div>`;
  }).join("") || `<div class="sub">这份结果里没有覆盖率。</div>`;
  const usable = cov.reduce((s, c) => s + (c.within_threshold || 0), 0);
  const fresh = cov.reduce((s, c) => s + (c.within_2h || 0), 0);
  const extra = (res.extra_item_ids || []).length;
  const extraPairs = res.capture?.extra_pairs || 0;
  const capUsed = res.capture ? (res.capture.asks_used || 0) + (res.capture.bids_used || 0) : 0;
  $(ids.note).textContent = `${num(items)} 个物品${extra ? `(其中 ${num(extra)} 个是抓包并进来的)` : ""}、` +
    `${num(pairs)} 个物品×品质组合${extraPairs ? `(其中 ${num(extraPairs)} 个是抓包并进来的)` : ""} × 2 边 × ${cov.length} 城 = ${num(max * cov.length)} 个可能的报价点 · ` +
    `${num(usable)} 个在 ${FRESH_MAX} 小时新鲜度内,其中 ${num(fresh)} 个在 ${FRESH_HI} 小时内` +
    (hasCap ? ` · 抓包盖掉 ${num(capUsed)} 个价` : "");
  $(ids.legend).innerHTML = `
    <span><i class="swatch" style="background:var(--bronze)"></i><b>${FRESH_HI} 小时内</b></span>
    <span><i class="swatch swatch-city"></i><b>${FRESH_HI}–${FRESH_MAX} 小时</b>(按城市着色)</span>
    <span><i class="swatch" style="background:var(--ink-soft);opacity:.18"></i>超出 ${FRESH_MAX} 小时</span>
    <span><i class="swatch" style="background:var(--surface-2)"></i>无人上传</span>` + (hasCap ? `
    <span><i class="swatch" style="background:var(--gain)"></i>细条:抓包价用上了</span>
    <span><i class="swatch" style="background:var(--gain);opacity:.3"></i>抓到了,但 AODP 更新</span>` : "");
}

// ── 被过滤的候选 ──
function renderRejects(res) {
  const counts = Object.entries(res.reject_counts || {}).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1]);
  const total = counts.reduce((a, [, n]) => a + n, 0);
  // 机会少的时候,"为什么这么少"才是最该看的信息,别藏在折叠里
  if (!rejectsTouched) $("i-rejects").open = rejectsAuto = ideas.length < 5;
  $("i-reject-total").textContent = total
    ? `${num(total)} 条 — 这些就是天真计算器会报成机会的那些。点格子看明细` : "这一轮没有候选被拦下";
  if (rejectPick && !counts.some(([k]) => k === rejectPick)) rejectPick = "";
  $("i-reject-grid").innerHTML = counts.map(([k, n]) =>
    `<button type="button" data-reason="${esc(k)}" aria-pressed="${k === rejectPick}"><b>${num(n)}</b><span>${esc(rejectLabel(k))}</span></button>`).join("");
  renderRejectList(res);

  const get = k => res.reject_counts?.[k] || 0;
  const coverage = get("one_sided") + get("stale") + get("no_timestamp") + get("future_timestamp");
  const troll = get("deviation") + get("implausible_margin") + get("crossed_book");
  const gate = get("no_bid_side") + get("thin_book") + get("wide_spread");
  const hints = [];
  if (total && coverage / total > 0.5) hints.push(
    `<b>${num(coverage)}/${num(total)}</b> 条死在数据覆盖率上(单边、过期或时间戳不对)——不是价差不存在,是没人上传。
     放宽新鲜度能看到更多,但那些价格实盘八成已经变了。要根治得有成员开着客户端在这几座城翻市场。`);
  if (troll) hints.push(
    `<b>${num(troll)}</b> 条被 troll 过滤拦下。AODP 不返回挂单数量,"最低卖价 1000"可能只对应 1 件货,
     这些正是买了就套死的挂单。`);
  if (!troll && total) hints.push(
    `troll 过滤这次一条都没拦到。高流动性品类上的恶意挂单会被真实挂单淹没,
     真正在起作用的是新鲜度和单边过滤。扩到低流动性物品时再回头看这个数。`);
  if (gate) hints.push(
    `<b>${num(gate)}</b> 条被深度闸门拦下(没人在收货 ${num(get("no_bid_side"))} / 挂单太薄 ${num(get("thin_book"))} /
     价差过宽 ${num(get("wide_spread"))})。闸门只看抓包那一边 —— AODP 不给件数,对它不生效。
     抓包看到的买方深度是真的:挂买单进去大概率一直挂着,还白付 2.5% 创建费。`);
  $("i-diagnosis").innerHTML = hints.map(h => `<div class="diagnosis">${h}</div>`).join("");
}

// 这一边判它时用了抓包没有
const rejCaptured = x => x.ask_source === "capture" || x.bid_source === "capture";

// 明细的筛选框:选项带这个原因下的条数,品质五档都列(某档 0 条也列,选着它就知道"这一类里没有")
function renderRejectFilter(rows) {
  const qs = [0, 0, 0, 0, 0, 0];
  for (const x of rows) if (x.quality >= 1 && x.quality <= 5) qs[x.quality]++;
  const cap = rows.filter(rejCaptured).length;
  const opt = (v, label, n, cur) => `<option value="${v}"${String(cur) === String(v) ? " selected" : ""}>${label}(${num(n)})</option>`;
  $("i-rq").innerHTML = opt(0, "全部", rows.length, rejectView.q) +
    [1, 2, 3, 4, 5].map(q => opt(q, QUALITY[q], qs[q], rejectView.q)).join("");
  $("i-rsrc").innerHTML = opt("", "全部", rows.length, rejectView.src) +
    opt("capture", "用了抓包", cap, rejectView.src) + opt("aodp", "纯 AODP", rows.length - cap, rejectView.src);
}

// 一边的价:有抓包徽标就是抓包抓到的那个价。老服务端不带价时只剩来源
const rejPrice = (p, src, side) => `<td title="${esc(`${side}来自 ${SRC_WORD[src] || src || "—"}`)}">${
  p ? num(p) : "—"}${src === "capture" ? `<i class="src">抓</i>` : ""}</td>`;

function renderRejectList(res, opts = {}) {
  const box = $("i-reject-list");
  const bar = $("i-reject-filter");
  if (!rejectPick) { box.innerHTML = ""; bar.hidden = true; return; }
  if (!Array.isArray(res.rejected)) {
    bar.hidden = true;
    box.innerHTML = `<p class="note">这份结果里没有逐条明细。</p>`;
    return;
  }
  const ofReason = res.rejected.filter(x => x.reason === rejectPick);
  bar.hidden = false;
  renderRejectFilter(ofReason);
  // 抓包参与判定的排前面,其次品质高的,其余保持服务端的顺序(配置清单在前)。
  // 以前原样截前 300 条:单边那一类有上千条,清单里的普通品质全排在前面,
  // 抓包抓到的良好~不凡一条都进不了前 300,机会页上等于看不到
  const all = ofReason
    .filter(x => (!rejectView.q || x.quality === rejectView.q) &&
      (!rejectView.src || (rejectView.src === "capture") === rejCaptured(x)))
    .map((x, i) => ({ x, i }))
    .sort((a, b) => (rejCaptured(b.x) - rejCaptured(a.x)) || ((b.x.quality || 0) - (a.x.quality || 0)) || (a.i - b.i))
    .map(o => o.x);
  const shown = all.slice(0, rejectView.limit);
  for (const x of shown) noteName(x.item_id, x.item_name);   // 以后服务端带上名字就直接用
  // rejected[] 不带名字,NAMES 只攒了机会板、查价、销量榜见过的:没见过的先显示 id,
  // 查完目录再重画一次(还停在同一个原因上才画)
  const pick = `${rejectPick}|${rejectView.q}|${rejectView.src}`;
  if (!opts.named) resolveNames(shown.map(x => x.item_id), 200).then(n => {
    if (n && `${rejectPick}|${rejectView.q}|${rejectView.src}` === pick && scan === res) renderRejectList(res, { named: true });
  });
  const y = box.dataset.pick === pick ? box.querySelector(".rlist")?.scrollTop || 0 : 0;
  box.dataset.pick = pick;
  box.innerHTML = !all.length
    ? `<p class="note">这一类里没有符合筛选的候选。</p>`
    : `<div class="rlist"><table class="tight"><thead><tr>
      <th class="l">物品</th><th class="l">城市</th><th class="l">品质</th>
      <th title="判它时用的卖一(最低卖价)。带「抓」的是成员抓包抓到的">卖一</th>
      <th title="判它时用的买一(最高买价)。带「抓」的是成员抓包抓到的">买一</th>
      <th class="l">为什么</th>
    </tr></thead><tbody>${shown.map(x => `<tr>
      <td class="l">${itemCell(x.item_id, NAMES.get(x.item_id) || x.item_id)}</td>
      <td class="l">${cityMark(x.city)}</td>
      <td class="l">${x.quality ? qualityTag(x.quality) : "—"}</td>
      ${rejPrice(x.ask_price, x.ask_source, "卖一")}
      ${rejPrice(x.bid_price, x.bid_source, "买一")}
      <td class="l d">${esc(x.detail || rejectLabel(x.reason))}</td>
    </tr>`).join("")}</tbody></table></div>` +
    (all.length > shown.length
      ? `<p class="sub">列了前 ${num(shown.length)} 条,共 ${num(all.length)} 条(抓包参与判定的、品质高的排在前面)。
         <button type="button" class="mini" data-more>再列 ${num(Math.min(REJECT_PAGE, all.length - shown.length))} 条</button></p>` : "");
  const list = box.querySelector(".rlist");
  if (list && y) list.scrollTop = y;   // 补上名字、结果刷新、再列一页时别把人滚回顶上
}

$("i-reject-grid").addEventListener("click", e => {
  const b = e.target.closest("button[data-reason]");
  if (!b || !scan) return;
  rejectPick = rejectPick === b.dataset.reason ? "" : b.dataset.reason;
  rejectView.limit = REJECT_PAGE;
  for (const x of $("i-reject-grid").children) x.setAttribute("aria-pressed", String(x.dataset.reason === rejectPick));
  renderRejectList(scan);
});
for (const id of ["i-rq", "i-rsrc"]) $(id).addEventListener("change", () => {
  rejectView.q = Number($("i-rq").value) || 0;
  rejectView.src = $("i-rsrc").value;
  rejectView.limit = REJECT_PAGE;
  if (scan) renderRejectList(scan);
});
$("i-reject-list").addEventListener("click", e => {
  if (!e.target.closest("button[data-more]") || !scan) return;
  rejectView.limit += REJECT_PAGE;
  renderRejectList(scan, { named: false });
});
// 代码改 open 也会派发 toggle,而且是异步派发的,不能靠"改之前打个标志"区分。
// 这里比开合状态和代码最后一次设的值:一样就是代码自己开合的,不一样才是用户点的。
// 以前一律当用户操作,第一次自动开合之后就再也不自动了
$("i-rejects").addEventListener("toggle", () => {
  if (scan && $("i-rejects").open !== rejectsAuto) rejectsTouched = true;
});

function renderIdeasFoot(res) {
  const when = s => (s ? new Date(s).toLocaleString("zh-CN") : "—");
  // 摘要没变时不重拉 /api/scan,手上这份的 evaluated_at 就停在上一次内容变的时候;
  // 重算时刻取推送里最新的那个(内容一样,只是又算了一遍)
  const ev = later(res.evaluated_at, sync.evalAt);
  const re = ev && ev !== res.started_at;
  $("i-foot").innerHTML =
    `<p>AODP 全量拉取于 ${when(res.started_at)}${re ? `,最近一次用抓包重算于 ${when(ev)}` : ""};
       ${num(res.price_rows)} 条 AODP 报价,${num(res.request_count)} 次 AODP 请求。</p>
     <p>往返的真实盈亏平衡价差:秒买秒卖 ${pct(beOf("taker-taker"), 2)}、秒买挂卖 ${pct(beOf("taker-maker"), 2)}、
       挂买秒卖 ${pct(beOf("maker-taker"), 2)}、挂买挂卖 ${pct(beOf("maker-maker"), 2)}。创建费下单就扣、不成交不退,
       挂上去没人来就是白付 2.5%,这一项模型里还没算。</p>
     <p>排序主键是日化绝对收益,不是利润率:毛利 30% 但一天只能做 3 件,不如 5% 但能做 2000 件。</p>`;
}
// 盈亏平衡优先用服务端 modes 里算好的
function beOf(mode) {
  for (const r of ideas) {
    const m = r.modes.find(x => x.mode === mode && x.breakeven != null);
    if (m) return m.breakeven;
  }
  return BREAKEVEN[mode];
}

function renderIdeasBanners(res) {
  const out = [];
  if (res.capture?.error)
    out.push(`<div class="banner"><b>抓包没融合全:</b>${esc(res.capture.error)}。受影响的部分这一轮退回了纯 AODP 的价。</div>`);
  if (ingestInfo?.location_conflicts > 0) out.push(conflictBanner(ingestInfo));
  if (scanError) out.push(`<div class="banner"><b>重新扫描失败:</b>${esc(scanError)}</div>`);
  $("i-banners").innerHTML = out.join("");
}

function renderIdeasPage(res, opts = {}) {
  $("i-lede").hidden = true;
  $("i-coverage").hidden = false;
  $("i-empty").hidden = true;
  $("i-results").hidden = false;
  renderCoverage(res, { grid: "i-cov-grid", note: "i-cov-note", legend: "i-cov-legend" });
  // 城市筛选:覆盖率里的城市(配置里的全部)加上机会里出现过的,保留当前选中
  const sel = $("i-city"), keep = sel.value;
  const cities = [...new Set([...(res.coverage || []).map(c => c.city),
    ...ideas.flatMap(r => [r.from_city, r.to_city])])].filter(Boolean);
  sel.innerHTML = `<option value="">全部</option>` +
    cities.map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join("");
  sel.value = cities.includes(keep) ? keep : "";
  for (const [id, m] of [["be-tt", "taker-taker"], ["be-tm", "taker-maker"], ["be-mt", "maker-taker"], ["be-mm", "maker-maker"]])
    $(id).textContent = pct(beOf(m), 2);
  renderIdeas(opts);
  renderRejects(res);
  renderIdeasFoot(res);
  renderIdeasBanners(res);
}

// 还没有结果时的空态:503 = 服务端还没扫完第一轮,不是错误
function renderIdeasEmpty(err) {
  $("i-lede").hidden = false;
  $("i-coverage").hidden = true;
  $("i-results").hidden = true;
  const box = $("i-empty");
  box.hidden = false;
  box.innerHTML = !err ? `<p>正在读取扫描结果…</p>`
    : err.status === 503
      ? `<h2>还没扫过</h2>
         <p>服务端启动后会自己跑第一轮 AODP 全量扫描,一般一两分钟。这一页会自己出结果,不用刷新。</p>
         <button class="act" data-rescan>立即扫描</button>
         <p class="empty-sub">拉取配置里全部物品在各城的当前挂单价和 30 天成交历史,再配上成员抓包传上来的挂单。</p>`
      : `<h2>读不到扫描结果</h2><p>${esc(err.message || err)}</p>
         <button class="act" data-rescan>重新扫描</button>
         <p class="empty-sub">服务端恢复之后这一页会自己重试。</p>`;
}
$("i-empty").addEventListener("click", e => { if (e.target.closest("[data-rescan]")) doRescan(); });

let scanError = "";
// 入库口径的计数(串城次数、最近一次现场)。新服务端每条 scan 通知都带(ScanEvent.ingest,
// 默认每分钟一条);/api/coverage 很贵,只在启动和全量扫描之后读,老服务端只有这一路
let ingestInfo = null;
// 换一份入库计数:串城计数或最近一次现场变了才重画两处横幅
function noteIngest(g) {
  if (!g || typeof g !== "object") return;
  const was = ingestInfo;
  ingestInfo = g;
  if (was && (was.location_conflicts || 0) === (g.location_conflicts || 0) &&
      (was.last_conflict?.at || "") === (g.last_conflict?.at || "")) return;
  if (scan) renderIdeasBanners(scan);
  renderDeskBanners(scan);
}
const conflictBanner = g => `<div class="banner"><b>多开串城:</b>入库时发现 ${num(g.location_conflicts)} 次同一张挂单被报成了不同城市。
  一台机器上多个客户端同时抓包、又分别在不同城市时,城市归属会串,那部分挂单的城市不可信。${
  g.last_conflict ? `最近一次:${esc(g.last_conflict.item_id)} 被报成 ${esc(g.last_conflict.location_id)}(原始地点 ${esc(g.last_conflict.raw_location_id)}),上报人 ${esc(g.last_conflict.reporter)}。` : ""}</div>`;

// 拿到一份新的扫描结果:两个页面一起换。why 只影响"要不要闪一下"。
// 推送消息本身不带 coverage / reject_counts / capture 这些,所以结果一律来自
// /api/scan(pullScan 或重新扫描),不直接拿推送消息画
function applyResult(res, why) {
  const had = !!scan;
  const prevFull = scan?.started_at || "";
  scan = res;
  scanError = "";   // 手上已经是更新的结果了,上一次「重新扫描失败」的提示不再成立
  sync.digest = res.digest || "";
  sync.stamp = scanStamp(res);
  scanBaseAt = Date.parse(res.evaluated_at || res.started_at) || Date.now();
  ideas = unify(res);
  for (const r of ideas) noteName(r.item_id, r.item_name);
  renderIdeasPage(res, { flash: had && why !== "boot" });
  renderDeskScan(res);
  loadPortfolio();
  noteEval(res.evaluated_at, res.started_at);
  liveTargetsChanged();   // 实时页订阅跟着机会板前几名换
  // AODP 全量换了,成交历史也跟着入库:销量榜、/api/coverage(城市列表、串城计数)补读一次。
  // 放在这里而不是推送回调里:老服务端没有推送、新服务端推送被丢时,新全量是轮询拉进来的,
  // 以前那条路一次都不补读
  if (had && prevFull && res.started_at && res.started_at !== prevFull) scheduleSlowReload();
}

// 服务端先发布扫描结果、后写成交历史(新服务端 flip.Scan 就是这个顺序),
// 一收到就读会读到入库之前的。延后几秒再读;/api/coverage 很贵,只读这一次
const SLOW_RELOAD_MS = 8000;
function scheduleSlowReload() {
  clearTimeout(sync.slowTimer);
  sync.slowTimer = setTimeout(() => {
    loadServerCoverage();
    if (currentView === "rank") loadRank();   // 不在销量榜就不用:切过去时 show() 会重读
  }, SLOW_RELOAD_MS);
}
// 同一份内容:有摘要比摘要,老服务端没有摘要就比时间戳
const sameResult = res => !!scan && (res.digest ? res.digest === sync.digest : scanStamp(res) === sync.stamp);

// 两处「重新扫描」(总览、机会页)和空态里的「立即扫描」走同一个
let rescanning = false;
async function doRescan() {
  if (rescanning) return;
  rescanning = true;
  const btns = [$("rescan"), $("i-rescan"), ...document.querySelectorAll("[data-rescan]")];
  for (const b of btns) { b.disabled = true; b.dataset.label = b.textContent; b.textContent = "扫描中…"; }
  try {
    const res = await getJSON("/api/scan", { method: "POST" });
    scanError = "";
    // 服务端在回 POST 之前就发布了这份结果:推送那一路多半已经拉过、画过同一份了,
    // 再 applyResult 一遍是整页重画两次、资金分配拉两次
    if (sameResult(res)) {
      noteEval(res.evaluated_at, res.started_at);
      renderIdeasBanners(scan);
      renderDeskBanners(scan);
    } else {
      applyResult(res, "rescan");
    }
  } catch (e) {
    scanError = String(e.message || e) + (e.status === 409 ? "(上一轮可能还在跑,跑完会自己出结果)" : "");
    if (scan) { renderIdeasBanners(scan); renderDeskScan(scan); } else { renderIdeasEmpty(e); renderDeskBanners(null); }
  } finally {
    rescanning = false;
    for (const b of btns) { b.disabled = false; if (b.dataset.label) b.textContent = b.dataset.label; }
  }
}
$("rescan").addEventListener("click", doRescan);
$("i-rescan").addEventListener("click", doRescan);

// 总览上和扫描有关的那几块
function renderScanMeta(res) {
  const extra = (res.extra_item_ids || []).length, missing = (res.missing_item_ids || []).length;
  const hm = s => new Date(s).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
  const ev = later(res.evaluated_at, sync.evalAt);   // 同 renderIdeasFoot
  const items = (res.item_ids || []).length, extraPairs = res.capture?.extra_pairs || 0;
  $("scan-meta").textContent =
    `${num(items)} 个物品${extra ? `(抓包并入 ${num(extra)})` : ""}` +
    // 组合数和物品数不一样时才写:扫描并进了抓到的别的品质
    (res.pairs && res.pairs !== items ? `・${num(res.pairs)} 个物品×品质${extraPairs ? `(抓包并入 ${num(extraPairs)})` : ""}` : "") +
    `${missing ? `・目录里查不到 ${missing} 个` : ""}・${num(res.price_rows)} 条报价・${num(res.request_count)} 次请求` +
    (res.started_at ? `・AODP 全量 ${hm(res.started_at)}` : "") +
    (ev && ev !== res.started_at ? `・最近重算 ${hm(ev)}` : "");
  $("scan-meta").title = missing ? "配置里有、目录里查不到的:" + res.missing_item_ids.join(", ") : "";
}
function renderDeskScan(res) {
  renderScanMeta(res);
  renderCoverage(res, { grid: "cov", note: "cov-note", legend: "cov-legend" });
  renderCaptureSummary(res.capture);
  renderCheckTable();
  renderDeskBanners(res);
}

function renderDeskBanners(res) {
  const out = [];
  if (scanError) out.push(`<div class="banner"><b>重新扫描失败:</b>${esc(scanError)}</div>`);
  if (res?.capture?.error)
    out.push(`<div class="banner"><b>抓包没融合全:</b>${esc(res.capture.error)}。受影响的部分这一轮退回了纯 AODP 的价。</div>`);
  if (ingestInfo?.location_conflicts > 0) out.push(conflictBanner(ingestInfo));
  $("desk-banners").innerHTML = out.join("");
}

// 抓包融合汇总。老服务端没有 capture 段,整块写一句说明
const CAP_FIELDS = [
  ["book_sides", "个盘口边抓到了挂单", c => `向库里要了 ${num(c.requested_keys)} 个盘口边`],
  ["asks_used", "个卖单簿用了抓包价"],
  ["bids_used", "个买单簿用了抓包价"],
  ["superseded", "个边 AODP 更新,抓包落选"],
  ["synthesized", "格只靠抓包补出来", () => "AODP 没返回这一格,全靠成员抓包"],
  ["ghosts", "张幽灵单被剔掉", () => "上一轮翻到、这一轮没再出现的单(多半已成交或撤单)"],
  ["conflict_keys", "个边最近串过城", () => "多开串城的盘口暂停了幽灵单剔除"],
  ["extra_items", "个物品因抓包并进扫描", c => (c.extra_dropped ? `超上限截掉 ${num(c.extra_dropped)} 个(名额按物品算)` : "名额按物品算,每个物品带上抓到的全部品质")],
  ["extra_pairs", "个物品×品质组合因抓包并进扫描", () => "配置清单里的物品抓到的别的品质,加上抓包并进来的物品抓到的品质。配置里的品质照扫,不算在内"],
  ["extra_pending", "个新组合等着补拉 AODP", c => "新抓到的物品,或者已有物品抓到的新品质;补拉之前不在机会板上" +
    (c.backfilled_at ? `。最近补拉 ${new Date(c.backfilled_at).toLocaleTimeString("zh-CN")}` : "")],
];
function renderCaptureSummary(c) {
  const box = $("cap-sum");
  if (!c || c.enabled === undefined) {
    box.innerHTML = `<div><span>这版服务端还不把抓包融合进扫描,机会板用的全是 AODP 的价。</span></div>`;
    return;
  }
  if (!c.enabled) {
    box.innerHTML = `<div><span>服务端配置里抓包融合关着(capture.enabled),机会板用的全是 AODP 的价。</span></div>`;
    return;
  }
  box.innerHTML = CAP_FIELDS.map(([k, label, tip]) =>
    `<div title="${esc(tip ? tip(c) : "")}"><b>${num(c[k] || 0)}</b><span>${label}</span></div>`).join("");
}

// 「待核对」:日收益靠前、但深度没核过的机会。不是深度不够,是不知道 ——
// 进游戏点进物品详情页翻一眼,下一轮重算就核上了
function renderCheckTable() {
  const rows = sortBy(ideas.filter(r => r.depth_checked === false), "daily_profit", "desc").slice(0, 10);
  const any = ideas.some(r => r.depth_checked !== undefined);
  $("check-note").textContent = !any ? "这版服务端的扫描结果里没有深度核验字段。"
    : rows.length ? "两条腿依托的那一边没有可信的抓包深度:进游戏照右边那列翻一眼,下一轮重算就核上了。"
      : "日收益靠前的机会深度都核过了。";
  const miss = r => [["买腿", r.buyLeg], ["卖腿", r.sellLeg]].filter(([, l]) => !l.side?.depth).map(([w]) => w);
  // 同一座城的两边合成一句:点进一次详情页两栏都抓到
  const guide = r => {
    const legs = miss(r);
    const byCity = new Map();
    for (const [w, l] of [["买腿", r.buyLeg], ["卖腿", r.sellLeg]]) {
      if (!legs.includes(w)) continue;
      const city = w === "买腿" ? r.from_city : r.to_city;
      const book = l.name.endsWith("ask") ? "出售订单" : "购入订单";
      if (!byCity.has(city)) byCity.set(city, new Set());
      byCity.get(city).add(book);
    }
    return [...byCity].map(([c, b]) => `到 ${c} 市场点进物品详情页,看「${[...b].join("」和「")}」`).join(";");
  };
  $("check-rows").closest("table").hidden = !rows.length;
  $("check-rows").innerHTML = rows.map(r => `<tr>
    <td class="l">${itemCell(r.item_id, r.item_name)}</td>
    <td class="l">${qualityTag(r.quality)}</td>
    <td class="l">${r.kind === "arb" ? `<span class="route">${cityMark(r.from_city)}<span>→</span>${cityMark(r.to_city)}</span>` : cityMark(r.from_city)}</td>
    <td class="l sub">${esc(r.mode_text)}</td>
    <td><b>${num(r.daily_profit)}</b></td>
    <td class="l">${miss(r).join("、") || "—"}</td>
    <td class="l sub" style="white-space:normal">${esc(guide(r))}</td>
  </tr>`).join("");
}

// 顶栏「行情」:最近一次评估(抓包快速重算,evaluated_at)离现在多久,老服务端没有就按
// AODP 全量(started_at)。两个都有而且不同时副行再写全量多久前。30 秒走一次,
// 以前只在拿到结果那一刻算一次,页面开一小时还写着"0 分钟前"
function renderScanAge() {
  const ev = sync.evalAt, full = sync.fullAt;
  if (!ev && !full) return;
  const ago = at => {
    const m = (Date.now() - Date.parse(at)) / 60000;
    return m < 1 ? "刚刚" : m < 60 ? `${Math.round(m)} 分钟前` : `${(m / 60).toFixed(1)} 小时前`;
  };
  const both = ev && full && ev !== full;
  $("scan-age").textContent = both ? `重算 ${ago(ev)}` : ago(ev || full);
  $("scan-age-sub").textContent = both ? `行情 · 全量 ${ago(full)}` : "行情";
  const when = s => new Date(s).toLocaleString("zh-CN");
  $("scan-age").closest(".fact").title = (full ? `AODP 全量拉取:${when(full)}` : "") +
    (both ? `\n最近一次用抓包重算:${when(ev)}` : "");
}
setInterval(renderScanAge, 30000);


// ── 总览:资金分配 ─────────────────────────────────────────
async function loadPortfolio() {
  const q = new URLSearchParams({
    capital: $("p-capital").value,
    max_per_slice: $("p-cap").value,
    risk: $("p-risk").value,
  });
  try {
    renderPlan(await getJSON("/api/portfolio?" + q));
  } catch (e) {
    $("p-note").textContent = String(e.message || e);
  }
}
for (const id of ["p-capital", "p-cap", "p-risk"]) $(id).addEventListener("change", loadPortfolio);

function renderPlan(p) {
  // 没扫过、或者一条都没分到时后端给的 slices 是 null。以前直接读 .length,
  // TypeError 的报错文字就显示在说明那一行
  const slices = p.slices || [];
  const idlePct = p.capital ? p.idle / p.capital : 0;
  $("kpis").innerHTML = `
    <div><b>${num(p.daily_profit)}</b><i>日收益(银)</i>
      <em>${slices.length} 个仓位</em></div>
    <div><b>${pct(p.daily_roi, 2)}</b><i>本金日回报</i>
      <em>年化没有意义,市场吃不下</em></div>
    <div><b>${num(p.deployed)}</b><i>已部署</i>
      <em>闲置 ${num(p.idle)}(${pct(idlePct, 0)})</em></div>
    <div><b>${num(slices.reduce((a, s) => a + (s.qty || 0), 0))}</b><i>总件数</i>
      <em>跨城 ${slices.filter(s => s.kind === "arb").length} 条</em></div>`;
  $("p-note").textContent = p.note || (slices.length ? "" : "这一轮扫描没有能分到钱的机会。");

  // payload 是整条机会/路线:可信度、深度核没核过都从它取,和机会页同一个口径
  const trust = pl => {
    if (!pl) return "—";
    const c = confClass(pl.confidence);
    const mists = (pl.risk_tags || []).includes("mists") ? ' <span class="tag risk">经迷雾</span>' : "";
    const chk = pl.depth_checked === false ? ' <span class="tag medium" title="两条腿的深度没都核过">未核深度</span>' : "";
    return `<span class="tag ${c}">${CONFIDENCE[pl.confidence] || "低"}</span>${chk}${mists}`;
  };
  // 同一物品同一城的不同品质是两个仓位,label 里没有品质:非普通的挂个品质标
  $("p-rows").innerHTML = slices.map(s => `<tr>
    <td class="l">${esc(s.label)}${s.payload?.quality > 1 ? " " + qualityTag(s.payload.quality) : ""}</td>
    <td class="l"><span class="kind ${s.kind === "arb" ? "arb" : "flip"}">${s.kind === "arb" ? "跨城" : "同城"}</span></td>
    <td>${num(s.qty)}</td>
    <td class="sub">${s.daily_qty != null ? num(s.daily_qty) : "—"}</td>
    <td>${num(s.capital)}</td>
    <td><b>${num(s.daily_profit)}</b></td>
    <td>${pct(s.roi)}</td>
    <td class="sub" title="风险调整后,排序按这个">${s.risk_adj_roi ? pct(s.risk_adj_roi) : "—"}</td>
    <td class="sub">${s.volatility ? s.volatility.toFixed(2) : "—"}</td>
    <td class="sub">${s.turns_per_day ? s.turns_per_day.toFixed(1) : "—"}</td>
    <td class="l">${trust(s.payload)}</td>
  </tr>`).join("");

  renderCurve(p);
}

// 累计投入 vs 累计收益。曲线越往右越平,就是边际收益递减——
// 一眼能看出"再投下去不值得"的拐点在哪
function renderCurve(p) {
  const pts = p.slices || [];
  if (!pts.length) { $("curve").innerHTML = ""; $("curve-note").textContent = ""; return; }
  const W = 400, H = 210, pad = { l: 8, r: 8, t: 12, b: 26 };
  const maxCap = pts[pts.length - 1].cum_capital || 1;
  const maxProfit = pts[pts.length - 1].cum_profit || 1;
  const x = v => pad.l + (v / maxCap) * (W - pad.l - pad.r);
  const y = v => H - pad.b - (v / maxProfit) * (H - pad.t - pad.b);

  const path = ["M " + x(0) + " " + y(0)]
    .concat(pts.map(s => `L ${x(s.cum_capital).toFixed(1)} ${y(s.cum_profit).toFixed(1)}`)).join(" ");
  const area = path + ` L ${x(maxCap).toFixed(1)} ${y(0)} Z`;
  // 直线参照:如果回报率一直保持第一条那么高,曲线会长这样
  const firstROI = pts[0].roi;
  const ideal = `M ${x(0)} ${y(0)} L ${x(maxCap).toFixed(1)} ${y(Math.min(maxCap * firstROI, maxProfit * 3)).toFixed(1)}`;

  $("curve").innerHTML = `<svg viewBox="0 0 ${W} ${H}" width="100%" height="${H}" role="img"
      aria-label="累计投入与累计日收益">
    <path d="${area}" fill="var(--bronze)" opacity=".14"/>
    <path d="${ideal}" stroke="var(--ink-soft)" stroke-width="1" stroke-dasharray="3 3" fill="none"/>
    <path d="${path}" stroke="var(--ink)" stroke-width="1.75" fill="none" stroke-linejoin="round"/>
    ${pts.map(s => `<circle cx="${x(s.cum_capital).toFixed(1)}" cy="${y(s.cum_profit).toFixed(1)}" r="3"
        fill="${s.kind === "arb" ? "var(--bronze)" : "var(--ink)"}"><title>${esc(s.label)}
累计投入 ${num(s.cum_capital)} → 累计日收益 ${num(s.cum_profit)}(${pct(s.cum_roi, 2)})</title></circle>`).join("")}
    <text x="${pad.l}" y="${H - 8}" font-size="10" fill="var(--ink-soft)">0</text>
    <text x="${W - pad.r}" y="${H - 8}" font-size="10" fill="var(--ink-soft)" text-anchor="end">${num(maxCap)} 银</text>
  </svg>`;
  // 说明必须和排序口径一致:排序用的是风险调整后的回报率,
  // 所以单调递减的是它,原始回报率不保证——波动大但账面高的
  // 品种被压到后面,它的原始 ROI 可能反而更高
  $("curve-note").textContent =
    `实线是实际累计收益,虚线是"回报率一直维持第一条水平"的理想情况。` +
    `两者岔开得越早,说明好机会越集中在前几条——` +
    `组合回报率从 ${pct(pts[0].roi, 1)} 摊薄到 ${pct(p.daily_roi, 2)}。` +
    `排序按风险调整后的回报率,所以那一列是递减的;` +
    `原始回报率可能有起伏(波动大的品种被往后压了)。`;
}

// ── 校准 ───────────────────────────────────────────────────
async function loadCalibration() {
  try {
    const d = await getJSON("/api/calibration");
    const c = d.calibration;
    if (!c.trades) {
      $("calib").innerHTML = `<p class="note">还没有成交记录。模型里的
        <code>absorb_ratio = ${d.current_absorb}</code> 是拍脑袋的值,
        记几笔真实成交之后这里会给出实测建议。</p>`;
      return;
    }
    $("calib").innerHTML = `<div class="kpis">
      <div><b>${c.trades}</b><i>记录笔数</i><em>已收口 ${c.closed}</em></div>
      <div><b>${pct(c.fill_rate)}</b><i>成交率</i><em>计划量里实际吃到的比例</em></div>
      <div><b>${c.has_suggestion ? c.suggested_absorb_ratio.toFixed(3) : "—"}</b>
        <i>实测 absorb_ratio</i><em>当前配置 ${d.current_absorb}</em></div>
      <div><b>${c.has_accuracy ? pct(c.accuracy) : "—"}</b><i>模型准确度</i>
        <em>实际净利 ${num(c.realized_profit)} / 计划净利 ${num(c.planned_profit)}</em></div>
    </div>`;
  } catch (e) { $("calib").innerHTML = `<p class="note">${esc(e.message || e)}</p>`; }
}

// ── 记账 ───────────────────────────────────────────────────
let pendingTrade = null;

// 按行 key 找那一条,不按行号:结果随时会刷新重排,行号对不上就记成了另一条
function openTrade(key) {
  const r = ideas.find(x => x.key === key);
  if (!r) { alert("这条机会在最新的扫描结果里已经没有了。"); return; }
  pendingTrade = r;
  $("td-title").textContent = "记一笔:" + r.item_name;
  $("td-sub").textContent =
    `${r.mode_text}・${QUALITY[r.quality] || ""}・${r.from_city}${r.kind === "arb" ? " → " + r.to_city : ""}・` +
    `买 ${num(r.buy_price)} 卖 ${num(r.sell_price)}`;
  const form = $("trade-form");
  form.owner.value = localStorage.getItem("owner") || "";
  form.qty.value = r.qty;
  $("trade-dialog").showModal();
}

$("trade-form").addEventListener("submit", async e => {
  if (e.submitter?.value !== "ok" || !pendingTrade) return;
  const f = e.target, r = pendingTrade;
  localStorage.setItem("owner", f.owner.value);
  try {
    await getJSON("/api/trades", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        owner: f.owner.value, item_id: r.item_id, quality: r.quality,
        kind: r.kind, buy_city: r.from_city, sell_city: r.to_city, mode: r.mode,
        planned_qty: +f.qty.value, planned_buy: r.buy_price, planned_sell: r.sell_price,
        // 以前按 物品 + 城市 找,没带品质:同一物品多个品质时会拿错那一条的成交量
        market_daily_qty: r.kind === "arb" ? 0 : r.daily_volume_qty || 0,
        note: f.note.value,
      }),
    });
    $("t-owner").value = f.owner.value;
    loadCalibration();
  } catch (err) { alert("记账失败:" + err.message); }
});

async function loadBook() {
  const owner = $("t-owner").value || localStorage.getItem("owner") || "";
  $("t-owner").value = owner;
  const q = new URLSearchParams({ owner, status: $("t-status").value });
  try {
    const rows = await getJSON("/api/trades?" + q);
    await resolveNames(rows.map(t => t.item_id));
    $("book-empty").style.display = rows.length ? "none" : "";
    $("book-meta").textContent = `${rows.length} 笔`;
    const pair = (a, b) => (a != null || b != null ? `${a != null ? num(a) : "—"} / ${b != null ? num(b) : "—"}` : "—");
    const day = s => (s ? new Date(s).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "");
    $("book-rows").innerHTML = rows.map(t => `<tr>
      <td class="l">${itemCell(t.item_id, NAMES.get(t.item_id) || t.item_id)}${t.note ? `<div class="sub" title="${esc(t.note)}">${esc(t.note)}</div>` : ""}</td>
      <td class="l">${qualityTag(t.quality)}</td>
      <td class="l">${t.kind === "arb"
        ? `<span class="route">${cityDot(t.buy_city)}<span>→</span>${cityDot(t.sell_city)}</span>`
        : cityDot(t.buy_city)}</td>
      <td class="l sub" title="${esc(t.mode)}">${esc(MODE_LABEL[t.mode] || t.mode)}</td>
      <td>${num(t.planned_qty)}</td>
      <td>${num(t.planned_buy)}</td>
      <td>${num(t.planned_sell)}</td>
      <td title="买入 / 卖出件数">${pair(t.filled_buy_qty, t.filled_sell_qty)}</td>
      <td class="sub" title="买入 / 卖出均价">${pair(t.filled_buy_price, t.filled_sell_price)}</td>
      <td class="${t.realized_profit > 0 ? "pos" : t.realized_profit < 0 ? "neg" : ""}">
        ${t.realized_profit != null ? num(t.realized_profit) : "—"}</td>
      <td class="l"><span class="tag ${t.status === "filled" ? "high" : t.status === "open" ? "medium" : "low"}">${esc(STATUS[t.status] || t.status)}</span></td>
      <td class="l sub">${day(t.opened_at)}${t.closed_at ? ` → ${day(t.closed_at)}` : ""}</td>
      <td class="l">${t.status === "open"
        ? `<button class="mini" data-close="${Number(t.id)}" data-qty="${Number(t.planned_qty)}">收口</button> ` : ""
        }<button class="mini" data-del="${Number(t.id)}">删</button></td>
    </tr>`).join("");
    loadCalibration();
  } catch (e) {
    $("book-empty").textContent = String(e.message || e);
    $("book-empty").style.display = "";
  }
}
const STATUS = { open: "未收口", filled: "已成交", partial: "部分成交", abandoned: "放弃" };

// 记账接口、被拒明细都只有 item_id。扫描、查价里见过的名字直接用;没见过的按 id 搜一次目录,
// 一次最多查 max 个、同时最多 4 个请求,查不到的就显示 id。目录里确实没有的记下来,
// 下次不再白查(没同步 / 网络失败不算,下次还查)
const NAME_MISS = new Set();
async function resolveNames(ids, max = 20) {
  const want = [...new Set(ids)].filter(id => id && !NAMES.has(id) && !NAME_MISS.has(id)).slice(0, max);
  let next = 0;
  const worker = async () => {
    while (next < want.length) {
      const id = want[next++];
      try {
        const hits = await getJSON(`/api/items?limit=5&q=${encodeURIComponent(id)}`);
        const it = (hits || []).find(x => x.item_id === id);
        if (it && displayName(it) !== id) noteName(id, displayName(it)); else NAME_MISS.add(id);
      } catch (e) { /* 目录没同步:显示 id */ }
    }
  };
  await Promise.all(Array.from({ length: Math.min(4, want.length) }, worker));
  return want.length;
}

for (const id of ["t-owner", "t-status"]) $(id).addEventListener("change", loadBook);

let closingID = null;
$("book-rows").addEventListener("click", async e => {
  const del = e.target.closest("button[data-del]");
  if (del) {
    // 留着一笔错的记录比没有更糟——校准是拿它们反推模型的
    if (!confirm("删掉这笔记录?校准会重新计算。")) return;
    const owner = encodeURIComponent($("t-owner").value || localStorage.getItem("owner") || "");
    try {
      await getJSON(`/api/trades/${del.dataset.del}?owner=${owner}`, { method: "DELETE" });
      loadBook();
    } catch (err) { alert("删除失败:" + err.message); }
    return;
  }
  const b = e.target.closest("button[data-close]");
  if (!b) return;
  closingID = +b.dataset.close;
  $("cd-sub").textContent = `计划 ${b.dataset.qty} 件。没成交就填 0,一样是有效数据——放弃的单子才最能说明模型高估在哪。`;
  const f = $("close-form");
  f.buy_qty.value = b.dataset.qty; f.sell_qty.value = b.dataset.qty;
  f.buy_price.value = ""; f.sell_price.value = "";
  $("close-dialog").showModal();
});

$("close-form").addEventListener("submit", async e => {
  if (e.submitter?.value !== "ok" || !closingID) return;
  const f = e.target;
  const buyQty = +f.buy_qty.value, sellQty = +f.sell_qty.value;
  const buyPrice = +f.buy_price.value, sellPrice = +f.sell_price.value;
  // 净利 = 卖出收入扣税 - 买入支出。挂单的创建费在下单时就付了,
  // 这里按最常见的口径只扣市场税,想精确就自己改备注
  const realized = Math.round(sellQty * sellPrice * (1 - MARKET_TAX) - buyQty * buyPrice);
  try {
    await getJSON(`/api/trades/${closingID}/close`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        owner: $("t-owner").value || localStorage.getItem("owner") || "",
        filled_buy_qty: buyQty, filled_buy_price: buyPrice,
        filled_sell_qty: sellQty, filled_sell_price: sellPrice,
        realized_profit: realized,
      }),
    });
    loadBook();
  } catch (err) { alert("收口失败:" + err.message); }
});

// ── 查价 ───────────────────────────────────────────────────
// 旧版 web 端的三栏:左物品列表 | 中间 城市 × 品质 价格矩阵 | 右侧固定挂单簿。
// 矩阵走 /api/lookup/grid,阶梯走 /api/lookup/book。右栏的成交统计和走势图
// **直接用矩阵里同一格的 history 对象**,卡片和面板因此是同一份数、同一个来源。
const lookup = {
  itemId: null, seq: 0, data: null,          // seq:连点物品只认最后一次回来的
  pending: false, loadedAt: 0,               // 请求在飞 / data 是什么时候拿到的(本机时钟)
  list: [], listSeq: 0, listTouched: false,
  sel: { cat: "", sub: "", fam: "" },
  timer: null,
};
// 同一件物品在这个时间内再被路由到,直接用手上那份,不重查
const LOOKUP_REUSE_MS = 60000;
// 点格子时价格表和阶梯一起重拉。表最多等这么久:AODP 缓存过期又撞上限流时
// grid 要十几秒,右栏不能一直转圈 —— 等不到就先用手上那份画,回来再整体重画
const GRID_WAIT_MS = 2500;
// 右栏。和记账页的 loadBook/book-rows 是两回事,名字特意分开。
// gridLate:这次跟着重拉的价格表还没回来,面板顶部的数暂时是旧的
const ladder = { key: "", itemId: "", city: "", quality: 0, seq: 0, data: null, gridLate: false };

const iconURL = id => `/api/icon/${encodeURIComponent(id)}`;
const displayName = it => (it.name_zh || it.name_en || it.item_id) + (it.enchantment ? "." + it.enchantment : "");
const silver = v => {
  const a = Math.abs(v);
  return a >= 1e6 ? (v / 1e6).toFixed(2) + "M" : a >= 1e3 ? (v / 1e3).toFixed(1) + "k" : num(v);
};
const ageText = h => h == null ? "" : h.toFixed(1) + "h";
const spanText = h => h == null ? "" : h < 1 ? Math.max(1, Math.round(h * 60)) + "m"
  : h < 48 ? Math.round(h) + "h" : Math.round(h / 24) + "d";
const cityShort = c => c === "Fort Sterling" ? "Ft Sterling" : c;
const cityMark = c => `<span class="city"><i class="cdot" style="--c:var(${CITY_VAR[c] || "--ink-soft"})"></i>${esc(cityShort(c))}</span>`;
const lookupCell = (d, city, q) => (d?.cells || []).find(c => c.city === city && c.quality === q) || null;

// 等级 / 附魔 / 品质三个下拉是固定的,不用等接口
(function initLookupFilters() {
  for (let t = 1; t <= 8; t++) $("f-tier").add(new Option(`T${t}`, String(t)));
  for (let e = 0; e <= 4; e++) $("f-ench").add(new Option(e ? `.${e}` : "无附魔", String(e)));
  for (let q = 1; q <= 5; q++) $("f-qual").add(new Option(QUALITY[q], String(q)));
})();

// ── 左栏:物品列表 ──
// reveal:按当前物品的分类自动填的左栏,填完把选中那一项滚进列表的可见范围
async function refreshList(opts = {}) {
  const seq = ++lookup.listSeq;
  const q = $("search").value.trim();
  // 搜索和分类是互斥的两条路,置灰比只写一句 placeholder 说得清楚。
  // 品质不置灰:它只筛右边的矩阵
  for (const id of ["menu-btn", "f-tier", "f-ench"]) $(id).disabled = !!q;
  if (!menuTree) { try { menuTree = await getJSON("/api/menu"); } catch (e) { /* 目录还没同步 */ } }
  $("menu-path").textContent = menuPath();
  let rows = [], head;
  try {
    if (q) {
      rows = await getJSON(`/api/items?limit=200&q=${encodeURIComponent(q)}`);
      head = `搜索「${q}」— ${rows.length} 个`;
    } else if (lookup.sel.cat || $("f-tier").value !== "0" || $("f-ench").value !== "-1") {
      const p = new URLSearchParams({
        category: lookup.sel.cat, subcategory: lookup.sel.sub, family: lookup.sel.fam,
        tier: $("f-tier").value, enchant: $("f-ench").value, limit: "300",
      });
      rows = await getJSON("/api/items?" + p);
      head = `${menuPath()} — ${rows.length} 个${rows.length >= 300 ? "(已截断)" : ""}`;
    } else {
      head = "从上面的分类挑,或者直接搜";
    }
  } catch (e) {
    head = String(e.message || e);
  }
  if (seq !== lookup.listSeq) return;   // 打字快的时候,旧的搜索结果晚到不能盖掉新的
  lookup.listTouched = !!(q || lookup.sel.cat || $("f-tier").value !== "0" || $("f-ench").value !== "-1");
  lookup.list = rows;
  $("list-head").textContent = head;
  renderList();
  const list = $("list");
  list.scrollTop = 0;
  const on = opts.reveal && list.querySelector("button.on");
  // 只滚左栏自己,不用 scrollIntoView:那个会连带把整页也滚走
  if (on) list.scrollTop = on.getBoundingClientRect().top - list.getBoundingClientRect().top - 40;
}

function renderList() {
  $("list").innerHTML = lookup.list.length
    ? lookup.list.map(it => `<button data-id="${esc(it.item_id)}" class="${it.item_id === lookup.itemId ? "on" : ""}">
        <img src="${iconURL(it.item_id)}" alt="" loading="lazy" onerror="this.style.visibility='hidden'">
        <span class="tierbadge">T${Number(it.tier) || "?"}</span>
        <span class="names"><span class="zh">${esc(displayName(it))}</span><span class="en">${esc(it.name_en || it.item_id)}</span></span>
      </button>`).join("")
    : lookup.listTouched ? `<div class="empty"><p>没有匹配的物品。</p></div>` : "";
}
// 选中只挪高亮,不整列重绘:重绘会把一整列图标重新请求一遍
function markListSelection() {
  for (const b of $("list").querySelectorAll("button[data-id]"))
    b.classList.toggle("on", b.dataset.id === lookup.itemId);
}
$("list").addEventListener("click", e => {
  const b = e.target.closest("button[data-id]");
  if (b) selectItem(b.dataset.id);
});
$("search").addEventListener("input", () => {
  clearTimeout(lookup.timer);
  lookup.timer = setTimeout(refreshList, 220);
});
for (const id of ["f-tier", "f-ench"]) $(id).addEventListener("change", refreshList);
// 品质只在前端重画,不用再请求
$("f-qual").addEventListener("change", () => { if (lookup.data) renderPrices(lookup.data); });

// ── 中栏:价格矩阵 ──
// opts.live:实时刷新(推送、重连、切回本页补拉)。只重拉这一件的表和右栏,
// 不动地址栏、不动左栏 —— 以前复用完整的选中流程,用户一边开着这件、一边在左栏
// 按分类翻别的物品,一条推送过来左栏就被拉回这件物品的分类、滚动归零
async function selectItem(itemId, opts = {}) {
  if (!itemId) return;
  const seq = ++lookup.seq;
  // 同一件重查(页面开久了、从别的页跳回来)时旧表先留着,新数据回来再换,别闪成"正在查"
  const again = itemId === lookup.itemId && !!lookup.data;
  lookup.itemId = itemId;
  lookup.pending = true;
  if (!again) lookup.data = null;
  if (!opts.live) {
    markListSelection();
    const h = "#lookup/" + encodeURIComponent(itemId);
    if (location.hash !== h) location.hash = h;   // route() 看到请求在飞会跳过
  }
  $("lookup-empty").hidden = true;
  const box = $("lookup-result");
  box.hidden = false;
  if (!again) box.innerHTML = `<div class="empty"><p>正在查 ${esc(itemId)} …</p></div>`;
  // 旧右栏先留着,新数据回来、确认换了物品再收(renderPrices 里处理)
  try {
    const d = await getJSON("/api/lookup/grid?item=" + encodeURIComponent(itemId));
    if (seq !== lookup.seq) return;   // 快速切物品时先发的请求可能晚到
    lookup.pending = false;
    lookup.data = d;
    lookup.loadedAt = Date.now();
    noteName(d.item?.item_id, displayName(d.item || {}));
    renderPrices(d);
    lookupSubscribe(d);
    // 同一件重查过了,右栏的阶梯也得跟着换成这一刻的,不然面板上下两半是两个时间的数
    if (ladder.key && ladder.itemId === itemId) loadLadder(++ladder.seq, { grid: false });
    // 从别的页面点物品名跳过来、这件又不在左栏里:照它的分类重填左栏。
    // 每次都要判断,不是只有第一次 —— 旧版 gotoItem 就是每次都重填。
    // 用户自己在搜索框里搜着的时候不动;实时刷新不动(见函数头)
    const inList = lookup.list.some(it => it.item_id === itemId);
    if (!opts.live && !inList && !$("search").value.trim() && d.item?.category) {
      lookup.sel = { cat: d.item.category, sub: d.item.subcategory || "", fam: d.item.family || "" };
      refreshList({ reveal: true });
    }
  } catch (err) {
    if (seq !== lookup.seq) return;
    lookup.pending = false;
    // 实时刷新那次失败(服务端抖了一下)就留着手上那份,不把整张表换成"查不到"
    if (again) return;
    lookup.data = null;
    box.innerHTML = `<div class="empty"><h2>查不到</h2><p>${esc(err.message || err)}</p></div>`;
    closeLadder();
    lookupSubscribe(null);
  }
}

// ── 查价页的实时刷新 ──
// 订阅当前物品在所有查价城市 × 品质 × 两边的 key。黑市没有抓包,订了也不会有消息。
// 推送不带整张表需要的东西(件数分档、成交历史),收到就去重拉 grid:
// 第一条到了之后等一会儿把同一波的都攒上,再拉一次。右栏开着的话挂单簿一起刷新。
// 等 2.5 秒而不是更短:老服务端的推送发在单子落库之前(入库 2 秒一轮 flush),
// 等得比 flush 短,重拉 grid 会在写库前读走旧数据,页面就停在上一个价上
const LOOKUP_LIVE_MS = 2500;
function lookupSubscribe(d) {
  const keys = [];
  if (d?.item?.item_id) {
    const qs = d.qualities?.length ? d.qualities : [1];
    for (const c of d.cities || []) {
      if (c === "Black Market") continue;
      for (const q of qs) for (const s of [0, 1]) keys.push(`${d.item.item_id}|${c}|${q}|${s}`);
    }
  }
  rtSetKeys("lookup", keys, { snapshot: false });
  liveTargetsChanged();
}
// 挂在 rt.quoteFns 上(见实时模块)。快照不触发:那是刚订阅时补的,表本来就是新拉的
function onLookupQuote(q, pushed) {
  if (!pushed || !lookup.data || !rt.owners.get("lookup")?.keys.has(q.k)) return;
  lookupLiveKick();
}
function lookupLiveKick() {
  if (lookup.liveTimer) return;
  lookup.liveTimer = setTimeout(() => {
    lookup.liveTimer = null;
    // 不在查价页就先记着,切回来再拉:看不见的表没必要每几秒重拉一次
    if (currentView !== "lookup") { lookup.stale = true; return; }
    refreshLookupLive();
  }, LOOKUP_LIVE_MS);
}
function refreshLookupLive() {
  if (!lookup.itemId || !lookup.data) return;
  if (lookup.pending) { lookupLiveKick(); return; }   // 正在拉,拉完再来一轮
  // 右栏的吃单量输入框里正打着字:重画会把焦点冲掉,等他打完
  const a = document.activeElement;
  if (a && a.tagName === "INPUT" && $("bookpanel").contains(a)) { lookupLiveKick(); return; }
  lookup.stale = false;
  selectItem(lookup.itemId, { live: true });   // 同一件重查:旧表先留着,新数据回来再换;右栏的阶梯跟着换
}

function renderPrices(d) {
  const P = d.params || {};
  const maxH = P.max_hours ?? 6;
  const minBid = P.min_bid_depth ?? 20;
  const nearTxt = pct(P.near_pct ?? 0.05, 0);   // 近价窗口:服务端给的,以前文案里写死 5%
  const qualities = d.qualities?.length ? d.qualities : [1];
  const fresh = s => s && s.best > 0 && s.age_hours != null && s.age_hours <= maxH;
  // 择边的宽容度和深度闸门的可信窗口:服务端给的,老服务端没有就用默认值
  const slackMin = P.prefer_slack_minutes ?? 10;
  const depthH = P.depth_max_hours ?? 2;
  const pickRule = `抓包不比 AODP 旧 ${slackMin} 分钟以上、或者两边同价就用抓包,否则用 AODP`;

  // 最优点按品质分别算。不同品质是不同的商品:杰出品质的长袍卖得比普通贵三倍,
  // 那不是价差,那是另一件东西。只在新鲜数据里挑:30 小时前的低价没有意义
  const best = {};
  for (const q of qualities) {
    const cells = d.cells.filter(c => c.quality === q);
    const buyable = cells.filter(c => fresh(c.sell));
    const sellable = cells.filter(c => fresh(c.buy));
    best[q] = {
      buy: buyable.length ? buyable.reduce((a, b) => (b.sell.best < a.sell.best ? b : a)) : null,
      sell: sellable.length ? sellable.reduce((a, b) => (b.buy.best > a.buy.best ? b : a)) : null,
    };
  }

  // 全空的品质列不画,否则大部分物品是一张 80% 空白的表
  const single = (d.item.max_quality || 1) < 2 && qualities.length < 2;
  $("f-qual").disabled = single;     // 材料只有普通一档,品质筛选没意义
  if (single) $("f-qual").value = "0";
  const only = Number($("f-qual").value || 0);
  const pool = only ? [only] : qualities;
  const live = pool.filter(q => d.cells.some(c => c.quality === q && (c.sell.best || c.buy.best)));
  const shown = live.length ? live : pool.slice(0, 1);
  const hidden = only ? [] : qualities.filter(q => !shown.includes(q));

  const at = c => `${esc(cityShort(c.city))} · ${QUALITY[c.quality]}`;
  // 顶栏只能报一个品质,挑新鲜数据最多的那档
  const headline = qualities
    .map(q => ({ q, n: d.cells.filter(c => c.quality === q && (fresh(c.sell) || fresh(c.buy))).length }))
    .sort((a, b) => b.n - a.n)[0];
  const hb = headline?.n ? best[headline.q].buy : null;
  const hs = headline?.n ? best[headline.q].sell : null;

  const head = `<div class="lookup-head">
    <img src="${iconURL(d.item.item_id)}" alt="" onerror="this.style.visibility='hidden'">
    <div>
      <h2>${esc(displayName(d.item))}</h2>
      <div class="sub">${d.item.name_en ? esc(d.item.name_en) + " · " : ""}${esc(d.item.item_id)}</div>
    </div>
    <div class="verdict">
      <div><b>${hb ? num(hb.sell.best) : "—"}</b>
           <i>全服最便宜,在这买 ${hb ? at(hb) : "— 无新鲜数据"}</i></div>
      <div><b>${hs ? num(hs.buy.best) : "—"}</b>
           <i>全服出价最高,在这卖 ${hs ? at(hs) : "— 无新鲜数据"}</i></div>
    </div>
  </div>`;

  // 两路都有时,这一侧为什么用了那一路。规则和机会页卡片是同一个函数(scan.PickCapture):
  // 以前这里写的是"两路谁新用谁、一样新用抓包",抓包比 AODP 旧几分钟时照旧用抓包,和文案对不上
  const pickWhy = s => {
    if (s.pick !== "capture") return `抓包比 AODP 旧 ${slackMin} 分钟以上、价也不同,用 AODP`;
    if (s.aodp.age_hours == null) return "AODP 没有时间戳,用抓包";
    if (s.aodp.best === s.capture.best) return "两边同价,用抓包(数据龄取两边里新的那个)";
    return s.capture.age_hours <= s.aodp.age_hours ? "抓包比 AODP 新,用抓包"
      : `抓包比 AODP 旧,但没超过 ${slackMin} 分钟,仍用抓包`;
  };
  // 一侧的来源说明:两路都列出来,说清楚用的是哪一路、为什么
  const srcHint = s => {
    const lines = [];
    if (s.capture) lines.push(`抓包 ${num(s.capture.best)}(${ageText(s.capture.age_hours)} 前)· 最优档 ${num(s.capture.qty_at_best)} 件 / ${Number(s.capture.orders_at_best)} 单 · ${nearTxt} 以内 ${num(s.capture.qty_near)} 件`);
    if (s.capture?.prev_page_orders) lines.push(`最近一次只翻到了后面的页,前面 ${Number(s.capture.prev_page_orders)} 张单是更早那一页看到的,照样算在内`);
    if (s.aodp) lines.push(`AODP ${num(s.aodp.best)}(${s.aodp.age_hours == null ? "没有时间戳" : ageText(s.aodp.age_hours) + " 前"})`);
    if (s.capture && s.aodp) lines.push(`用的是${s.pick === "capture" ? "抓包" : "AODP"}:${pickWhy(s)}。规则:${pickRule},和机会页同一个规则`);
    if (s.depth || s.note) lines.push(`深度闸门:${gateText(s, depthH)}`);
    return lines.join("\n");
  };
  // 深度闸门对这一侧的看法(见 gateText)。上面 ×N/M 是整本簿的口径(全部当前档,不管多旧),
  // 两边都有数时一样,不一样的是"有没有数":最优档太旧时闸门不判,卡片上写着"未参与判定",
  // 格子上也得写,不能只给一个件数
  const gateLine = s => {
    const d = s.depth;
    if (d) {
      const cliff = d.gap_after_near >= 0.2;
      return `<div class="ln3${cliff ? " cliff" : ""}" title="${esc("深度闸门:" + gateText(s, depthH))}">闸门 近价 ${num(d.qty_near)}${d.truncated ? "+" : ""} 件 · ${Number(d.levels_near)} 档</div>`;
    }
    if (s.note) return `<div class="ln3 off" title="${esc("深度闸门没有判这一侧,机会页卡片上同一边也是这句")}">${esc(s.note)}</div>`;
    return "";
  };
  // 买卖两侧的时间戳常常差很远,各报各的龄。标签用游戏里市场那两个页签的说法
  const side = (label, s, hint, kind) => {
    const stale = s.best && (s.age_hours == null || s.age_hours > maxH);
    const src = srcHint(s);
    return `<div class="ln" title="${esc(hint + (src ? "\n\n" + src : ""))}"><u>${label}</u><b data-f="${kind}">${s.best ? num(s.best) : "—"}</b>
      <s class="${stale ? "stale" : ""}">${ageText(s.age_hours)}</s></div>`;
  };
  // 价格下面那一行:件数(只有抓包有)+ 这一侧最远的一档 + 来源徽标。
  // 最优档 / 最优价 5% 以内两个数都给 —— 前者判"这个价是不是一件货",
  // 后者判"我挂单进去有没有人接"
  const extra = (s, kind) => {
    const parts = [];
    const cap = s.capture;
    if (cap) {
      const dim = s.pick !== "capture";
      parts.push(`<em class="qty${dim ? " dim" : ""}" title="${esc(`最优档 ${num(cap.qty_at_best)} 件,最优价 ${nearTxt} 以内共 ${num(cap.qty_near)} 件` +
        (dim ? `\n抓包比 AODP 旧 ${slackMin} 分钟以上、价也不同,价格用的是 AODP,件数只作参考` : ""))}">×${num(cap.qty_at_best)}${cap.qty_near > cap.qty_at_best ? "/" + num(cap.qty_near) : ""}</em>`);
    }
    const far = s.pick === "capture" ? cap.far : s.pick === "aodp" ? s.aodp.far : 0;
    if (far && far !== s.best) {
      const word = kind === "sell" ? "最高" : "最低";
      const cut = s.pick === "capture" && cap.truncated;
      const t = s.pick === "capture"
        ? `这一侧抓到的${word}一张挂单` + (cut ? "。游戏一页 50 单,没翻完的话实际还要更远" : "")
        : `AODP 的 ${kind === "sell" ? "sell_price_max" : "buy_price_min,多半是 1 银那种占位单"}`;
      parts.push(`<span title="${esc(t)}">${word} ${num(far)}${cut ? "…" : ""}</span>`);
    }
    const badge = s.pick === "capture" ? `<b class="src" title="这一侧来自自建抓包">抓</b>` : "";
    return parts.length || badge ? `<div class="ln2">${parts.join(" · ")}${badge}</div>` : "";
  };
  // 成交那一段。口径和来源都写清楚:之前只写「均 xxx」,AODP 没历史的城市
  // 显示「均 —」,点开挂单簿里却有走势图,两边对不上
  const histLines = h => {
    if (!h) return `<div title="这一格 AODP 没有成交历史,库里也没有。">7日成交 —</div>`;
    const src = h.source === "capture" ? "自抓" : h.stored ? "库里存下的 AODP(这次没取到)" : "AODP";
    const t = `成交量加权均价,不含今天(不是当天价)。7 日窗口 ${h.days_7d} 天有数据,30 日窗口 ${h.days_30d} 天。来源:${src}`;
    // 7 天里一天成交都没有时,"日均 0 件"会被读成"没人买",其实是没人上传。
    // 这时报 30 日口径,并写明是 30 日
    const daily = h.days_7d ? `日均 ${num(h.daily_qty_7d)} 件` : `7 日无数据 · 30日日均 ${num(h.daily_qty_30d)} 件`;
    return `<div title="${esc(t)}">7日均 ${h.avg_7d ? num(h.avg_7d) : "—"} · 30日 ${num(h.avg_30d)}</div>
      <div title="${esc(t)}">${daily}${h.source === "capture" ? '<i class="hsrc">自抓</i>' : ""}</div>`;
  };

  const cellHTML = c => {
    const pick = best[c.quality] || {};
    const isBuy = pick.buy === c, isSell = pick.sell === c;
    const cls = ["cell", isBuy ? "best-buy" : "", isSell ? "best-sell" : ""].join(" ");
    const tags = [isBuy ? "本档最便宜" : "", isSell ? "本档出价最高" : ""].filter(Boolean).join(" · ");
    // 价差大得不真实时别用绿色 —— 那等于在暗示"好机会"。扫描页有 troll 过滤
    // 拦着这种数据,查价页展示原始值,就得靠标色提醒
    let spread = "";
    if (c.spread) {
      const m = c.spread.margin, sus = c.spread.suspect;
      const cls2 = sus ? "suspect" : m > 0 ? "up" : "down";
      const hint = sus
        ? `毛利率 ${pct(m)} 高得不真实,多半是某一侧挂了 troll 单(1 件货、价格离谱)。AODP 拿不到挂单数量,这种单看不出来只有一件`
        : `在这座城挂买单收货、再挂卖单出货,一轮下来的税后净毛利率。挂 ${num(c.spread.my_bid)} 收、挂 ${num(c.spread.my_ask)} 卖,单件净赚 ${num(c.spread.profit_per_unit)}`;
      spread = `<div><span class="spread ${cls2}" title="${esc(hint)}">同城价差 ${m > 0 ? "+" : ""}${(m * 100).toFixed(1)}%${sus ? " ⚠" : ""}</span></div>`;
    }
    const bc = c.buy.capture, sc = c.sell.capture;
    const thin = bc && bc.qty_near < minBid;
    const gap = bc && bc.gap_after_near >= 0.2;
    // 只抓到卖单这一侧 = 在「从集市购买」列表页翻的。那个页面根本不发买单请求,
    // 抓不到不等于没有。不说清楚的话,跟"真的没人收货"长得一样
    const half = sc && !bc && c.city !== "Black Market";
    const bidNote = half
      ? `<div class="ctag warn">只抓到卖单这一侧 —— 进游戏<b>点进物品详情页</b>(左右两栏那个)才会同时抓到买单</div>`
      : !bc ? ""
      : thin ? `<div class="ctag warn">买方只有 ${num(bc.qty_near)} 件在收${gap ? `,再往下断崖 ${pct(bc.gap_after_near, 0)}` : ""} —— 挂买单多半收不到货</div>`
      : gap ? `<div class="ctag warn">买一附近吃完,下一档就低 ${pct(bc.gap_after_near, 0)},最高买价没支撑</div>` : "";
    return `<div class="${cls}">
      ${side("卖单最低", c.sell, "市场上最便宜的那张卖单。你想马上买到货,就付这个价", "sell")}${extra(c.sell, "sell")}${gateLine(c.sell)}
      ${side("买单最高", c.buy, "市场上出价最高的那张买单。你想马上出货,就拿这个价", "buy")}${extra(c.buy, "buy")}${gateLine(c.buy)}
      <div class="meta">${spread}${histLines(c.history)}</div>
      ${tags ? `<div class="ctag">${tags}</div>` : ""}
      ${bidNote}
    </div>`;
  };

  // 黑市就是最后一行。有数据的格子都能点:没抓到挂单也能看成交走势
  const rows = d.cities.map(city => `<tr>
    <td>${cityMark(city)}</td>
    ${shown.map(q => {
      const c = lookupCell(d, city, q);
      if (!c) return `<td><div class="cell void">无数据</div></td>`;
      return `<td><button class="cellbtn" data-city="${esc(city)}" data-q="${q}"
               title="点开完整挂单簿和成交走势">${cellHTML(c)}</button></td>`;
    }).join("")}
    <td class="flex"></td>
  </tr>`).join("");

  const aodpWarn = d.aodp?.error
    ? `<div class="diagnosis warn"><b>AODP 这次没取全:</b>${esc(d.aodp.error)}。表里的抓包数据不受影响。</div>` : "";
  const others = Object.entries(d.capture?.other_locations || {});
  const otherTxt = others.length
    ? `抓包里另有 ${others.map(([k, v]) => `${esc(k)} ${Number(v)} 单`).join("、")} 来自没收敛成城市名的地点,没放进表里。` : "";

  // 整表重画会把横向滚动位置和焦点一起冲掉:点格子时价格表会跟着重拉、重画,
  // 五档品质的装备点了"杰出"那列,重画完又滚回最左边就白点了
  const oldWrap = $("lookup-result").querySelector(".matrix-wrap");
  const keep = oldWrap ? { x: oldWrap.scrollLeft, focus: oldWrap.contains(document.activeElement) } : null;
  // 同一件物品重画(实时刷新)时,变了的价闪一下:涨绿跌红。换了物品不闪
  const was = lookup.shown?.itemId === d.item.item_id ? lookup.shown.best : null;
  const bestNow = new Map();
  for (const c of d.cells) {
    bestNow.set(`${c.city}|${c.quality}|sell`, c.sell.best || 0);
    bestNow.set(`${c.city}|${c.quality}|buy`, c.buy.best || 0);
  }
  lookup.shown = { itemId: d.item.item_id, best: bestNow };

  $("lookup-result").innerHTML = head + aodpWarn + `
    <div class="hbar" hidden aria-hidden="true"><div></div></div>
    <div class="hbar-note" hidden></div>
    <div class="matrix-wrap"><table class="matrix">
      <thead><tr><th>城市</th>${shown.map(q =>
        `<th class="q" style="--q:var(--q${q})"><i class="qdot"></i>${QUALITY[q] || q}</th>`
      ).join("")}<th class="flex"></th></tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
    <div class="matrix-note">
      <p><b>卖单最低</b>就是游戏里市场「销售订单」页签最上面那一行 —— 别人挂着卖的最低价,
      你想马上买到货付的就是它。<b>买单最高</b>是「购买订单」页签最上面那一行 —— 别人挂着收的最高价,
      你想马上出货拿的就是它。<b>×N/M</b> 是最优档件数 / 最优价 ${nearTxt} 以内的件数,只有自建抓包拿得到;
      带 <i class="src" style="margin:0">抓</i> 的那一侧用的是抓包,没带的是 AODP:${pickRule} —— 和机会页卡片同一个规则。
      <b>闸门</b>那一行是机会页深度闸门对这一侧的判定,只信 ${depthH} 小时内看到的档;最优档比这更旧时写"未参与判定"。</p>
      <p>所以<b>卖单价总是比买单价高</b>,这段差就是倒爷的利润空间。<b>同城价差</b>已经替你把
      ${pct(P.friction ?? 0.09)} 的税和手续费扣掉了:在这座城挂买单收货、再挂卖单出货,
      一轮下来的净毛利率,为正就不亏。它已经是扣完费的数,别再拿它和盈亏平衡比 ——
      换成没扣费的毛价差(卖价 / 买价 − 1),要超过 ${pct(P.breakeven ?? 0.0963, 2)} 才为正。
      标<b class="warnish">⚠</b> 的是超过 ${((P.max_margin ?? 1) * 100).toFixed(0)}% 的价差 —— 这种数字通常不是机会,
      而是某一侧挂了 troll 单。</p>
      <p>标红的时间是超过 ${maxH} 小时新鲜度上限的报价。最低/最高标记只在新鲜数据里选,
      且只在同一品质内比 —— 不同品质是不同的商品。抓包只看最近 ${P.capture_window_hours ?? 6} 小时,
      上一轮浏览留下、这一轮没再出现的单已经剔掉。
      ${single ? "这类物品只有普通一档品质。"
        : hidden.length ? `${hidden.map(q => QUALITY[q]).join("、")}品质全无数据,已隐藏。` : ""}
      ${only ? `只显示${QUALITY[only]}品质。` : ""}
      ${otherTxt}
      查询于 ${new Date(d.fetched_at).toLocaleString("zh-CN")}。</p>
    </div>`;

  // 换物品了,右侧那份是上一个物品的,收掉;同一物品重画(切品质、重拉)就保持打开
  if (ladder.itemId && ladder.itemId !== lookup.itemId) closeLadder();
  else if (ladder.key) { markOpenCell(); if (ladder.data) renderLadder(); }

  const wrap = $("lookup-result").querySelector(".matrix-wrap");
  const bar = $("lookup-result").querySelector(".hbar");
  // 两条滚动条互相跟。赋同一个值不会再触发 scroll,不会来回弹。
  // 表自己一滚(拖上面那条、点格子时 revealCellX 横着挪)就重算"左右还藏着哪几列"
  bar.addEventListener("scroll", () => { wrap.scrollLeft = bar.scrollLeft; }, { passive: true });
  wrap.addEventListener("scroll", () => { bar.scrollLeft = wrap.scrollLeft; queueHbarNote(); }, { passive: true });
  // 先恢复横向滚动位置再算尺寸和提示:以前反过来,提示算的是 scrollLeft=0 时藏着的列
  if (keep) {
    wrap.scrollLeft = keep.x;
    if (keep.focus) wrap.querySelector(".cellbtn.open")?.focus({ preventScroll: true });
  }
  fitLookup();
  if (was) {
    for (const b of wrap.querySelectorAll(".cellbtn")) {
      for (const kind of ["sell", "buy"]) {
        const k = `${b.dataset.city}|${b.dataset.q}|${kind}`;
        const before = was.get(k), after = bestNow.get(k);
        if (before !== undefined && after !== before)
          flash(b.querySelector(`b[data-f="${kind}"]`), after > before ? "up" : "down");
      }
    }
  }
}
$("lookup-result").addEventListener("click", e => {
  const b = e.target.closest(".cellbtn");
  if (b) openLadder(b.dataset.city, Number(b.dataset.q), b);
});

// ── 查价页的尺寸:右栏高度、表格横向滚动条 ──
// 挂单簿是 sticky 的,但高度不能写死 100vh:页面在顶部时它从自己的位置(离视口顶两百多 px)
// 往下排,底部的走势图就落到视口外;滚到底时 sticky 被查价区下沿卡住,标题和「收起」
// 又被顶出视口。这里按"查价区在视口里露出来的那一段"算,两头都放得下。
function fitLookup() {
  if (currentView !== "lookup") return;
  const bp = $("bookpanel");
  if (!bp.hidden && getComputedStyle(bp).position === "sticky") {
    // 按查价区的内容框算(它有 padding-top),不然右栏底边会多出那 18px 落到视口外
    const body = $("lookup-body"), lb = body.getBoundingClientRect(), cs = getComputedStyle(body);
    const top = Math.max(14, lb.top + parseFloat(cs.paddingTop));
    const bottom = Math.min(innerHeight - 14, lb.bottom - parseFloat(cs.paddingBottom));
    bp.style.maxHeight = Math.max(240, Math.floor(bottom - top)) + "px";
  } else {
    bp.style.maxHeight = "";   // 窄屏下右栏排到下面,不 sticky,也不限高
  }
  // 右栏宽度变了(改窗口大小、滚动条出现)就按新宽度重画走势图,字号保持 1:1
  const box = bp.querySelector(".chartbox");
  if (box && !bp.hidden) {
    const w = Math.floor(box.clientWidth);
    const cur = +(box.querySelector("svg")?.viewBox.baseVal.width || 0);
    if (cur && w > 0 && Math.abs(w - cur) > 2) box.innerHTML = tradeChart(ladder.series, w);
  }
  const wrap = $("lookup-result").querySelector(".matrix-wrap");
  if (!wrap) return;
  const over = wrap.scrollWidth - wrap.clientWidth;
  const bar = $("lookup-result").querySelector(".hbar");
  const note = $("lookup-result").querySelector(".hbar-note");
  wrap.classList.toggle("hscroll", over > 1);
  bar.hidden = note.hidden = over <= 1;
  if (over > 1) {
    bar.firstElementChild.style.width = wrap.scrollWidth + "px";
    bar.scrollLeft = wrap.scrollLeft;
    hbarNote();
  }
}
// 告诉人左右两边还藏着哪几列,不然五档品质只看得到两档半,也不知道要滚。
// 按当前滚动位置算:钉在左边的城市列挡住的也算藏着;露出来不到一半的列算藏着(只露一条边看不到价)。
// 以前只在重画时算一次、只报右边,横着一滚就过时了,右栏打开后还会把正开着、看得见的那一列报成"右边还有"
function hbarNote() {
  const wrap = $("lookup-result").querySelector(".matrix-wrap");
  const note = $("lookup-result").querySelector(".hbar-note");
  if (!wrap || !note || note.hidden) return;
  const r = wrap.getBoundingClientRect();
  const pinned = wrap.classList.contains("hscroll")
    ? wrap.querySelector("thead th:first-child")?.getBoundingClientRect().width || 0 : 0;
  const lo = r.left + pinned, hi = r.right;
  const left = [], right = [];
  for (const th of wrap.querySelectorAll("thead th.q")) {
    const b = th.getBoundingClientRect();
    if (Math.min(b.right, hi) - Math.max(b.left, lo) >= b.width / 2) continue;
    (b.left + b.width / 2 < lo ? left : right).push(th.textContent.trim());
  }
  const parts = [left.length ? `左边还有 ${left.join("、")}` : "", right.length ? `右边还有 ${right.join("、")}` : ""].filter(Boolean);
  note.textContent = `表比这一栏宽${parts.length ? "," + parts.join(";") : ""}:拖上面这条横向滚动,或者用「品质」只看一档`;
}
let noteQueued = false;
function queueHbarNote() {
  if (noteQueued) return;
  noteQueued = true;
  requestAnimationFrame(() => { noteQueued = false; hbarNote(); });
}
let fitQueued = false;
function queueFit() {
  if (fitQueued) return;
  fitQueued = true;
  requestAnimationFrame(() => { fitQueued = false; fitLookup(); });
}
window.addEventListener("scroll", queueFit, { passive: true });
window.addEventListener("resize", queueFit);

// ── 右栏:完整挂单簿 ──
// 游戏里那两栏只显示塞得进屏幕的几行,这里给全,还带累计量、相对最优价的落差、
// 每档挂了多久。放在右侧固定栏:那块地方本来一直空着,而且不用滚页面
function markOpenCell() {
  for (const b of $("lookup-result").querySelectorAll(".cellbtn"))
    b.classList.toggle("open", !!ladder.key && `${b.dataset.city}/${b.dataset.q}` === ladder.key);
}

function closeLadder() {
  ladder.seq++;   // 还在飞的请求回来也别往已经收起的栏里写
  ladder.key = ""; ladder.itemId = ""; ladder.data = null; ladder.gridLate = false; ladder.series = [];
  $("bookpanel").innerHTML = "";
  $("bookpanel").hidden = true;
  $("lookup-body").classList.remove("with-book");
  markOpenCell();
  fitLookup();
}

function ladderQty() {
  let v = 1000;
  try { v = +localStorage.getItem("lookupQty") || 1000; } catch (e) { /* 存储被禁用就用默认值 */ }
  return Math.max(1, Math.round(v));
}

async function openLadder(city, quality, btn) {
  const key = `${city}/${quality}`;
  if (ladder.key === key && ladder.itemId === lookup.itemId) { closeLadder(); return; }   // 再点一次 = 收起
  Object.assign(ladder, { key, itemId: lookup.itemId, city, quality, data: null, gridLate: false });
  const seq = ++ladder.seq;   // 连点不同格子时只认最后一次
  const panel = $("bookpanel");
  panel.hidden = false;
  $("lookup-body").classList.add("with-book");
  markOpenCell();
  panel.innerHTML = `<div class="bookwrap"><div class="void">读取中…</div></div>`;
  fitLookup();
  revealCellX(btn);
  await loadLadder(seq, { grid: true, top: true });
}

// 右栏挤出来以后中栏变窄,点的那一格可能被挤到横向滚动区外面。只横着滚表自己:
// scrollIntoView 会连带把整页竖着滚走,点一下格子页面跳一截
function revealCellX(btn) {
  const wrap = btn?.closest(".matrix-wrap");
  if (!wrap) return;
  const w = wrap.getBoundingClientRect(), c = btn.getBoundingClientRect();
  const pinned = wrap.classList.contains("hscroll")
    ? wrap.querySelector("tbody td:first-child")?.getBoundingClientRect().width || 0 : 0;
  if (c.right > w.right) wrap.scrollLeft += c.right - w.right + 8;
  else if (c.left < w.left + pinned) wrap.scrollLeft -= w.left + pinned - c.left + 8;
}

// 阶梯(/api/lookup/book)和价格表(/api/lookup/grid)**一起重拉**,两份都回来再画。
// 面板顶部的最低卖价、最高买价、挂买→挂卖、成交位置诊断取的是表里同一格;
// 表要是还是选物品那一刻拉的,成员一边在游戏里翻、一边开着这页,
// 顶部照抄的价和下面阶梯的第一档就会对不上。
// opts.grid:要不要跟着重拉价格表(selectItem 刚拉过就不用);opts.top:画完滚回顶部
async function loadLadder(seq, opts = {}) {
  const itemId = ladder.itemId;
  const p = new URLSearchParams({
    item: itemId, city: ladder.city, quality: ladder.quality, qty: ladderQty(),
  });
  const bookP = getJSON("/api/lookup/book?" + p);
  const gridP = opts.grid ? getJSON("/api/lookup/grid?item=" + encodeURIComponent(itemId)) : null;
  gridP?.catch(() => { /* 表没拉到就用手上那份,面板里会标出时间差 */ });
  let book;
  try {
    book = await bookP;
  } catch (err) {
    if (seq !== ladder.seq) return;
    $("bookpanel").innerHTML = `<div class="bookwrap"><div class="bookhead"><span class="spacer"></span>
      <button class="mini" data-close>收起</button></div>
      <div class="void" style="color:var(--warn)">${esc(err.message || err)}</div></div>`;
    return;
  }
  let grid = null;
  if (gridP) {
    const timeout = new Promise(r => setTimeout(() => r("late"), GRID_WAIT_MS));
    grid = await Promise.race([gridP.catch(() => null), timeout]);
  }
  if (seq !== ladder.seq) return;
  ladder.data = book;
  ladder.gridLate = grid === "late";
  const useGrid = g => {
    if (!g || g === "late" || lookup.itemId !== itemId) return false;
    lookup.data = g;
    lookup.loadedAt = Date.now();
    return true;
  };
  if (useGrid(grid)) renderPrices(grid);   // renderPrices 会连右栏一起画
  else renderLadder();
  if (opts.top) $("bookpanel").scrollTop = 0;
  if (ladder.gridLate) {
    gridP.then(g => {
      if (seq !== ladder.seq) return;
      ladder.gridLate = false;
      if (useGrid(g)) renderPrices(g); else renderLadder();
    }, () => {
      if (seq !== ladder.seq) return;
      ladder.gridLate = false;
      renderLadder();
    });
  }
}
$("bookpanel").addEventListener("click", e => {
  if (e.target.closest("[data-close]")) closeLadder();
  else if (e.target.closest("[data-reload]") && ladder.key) loadLadder(++ladder.seq, { grid: true });
});
$("bookpanel").addEventListener("change", e => {
  if (e.target.id !== "ld-qty") return;
  const v = Math.max(1, Math.round(+e.target.value || 1000));
  try { localStorage.setItem("lookupQty", String(v)); } catch (err) { /* 只是记不住而已 */ }
  if (ladder.key) loadLadder(++ladder.seq, { grid: true });
});

function ladderRows(levels, kind, cap) {
  if (!levels.length) return `<tr><td colspan="5" class="void">没有挂单</td></tr>`;
  return levels.map(l => {
    const w = Math.min(100, l.qty / cap * 100).toFixed(1);
    const off = l.off * 100;
    // 断崖标红:这一档比最优价低/高 20% 以上,说明上面那几档没有支撑
    const cliff = !l.stale && off >= 20;
    const offTxt = off >= 0.005 ? (kind === "sell" ? "+" : "−") + off.toFixed(off < 10 ? 2 : 1) + "%" : "";
    const t = `第一次看到是 ${l.standing_hours.toFixed(1)} 小时前,最后一次看到是 ${l.age_hours.toFixed(1)} 小时前` +
      (l.stale ? "\n上一轮翻得更深才看到的单,这一轮没翻到这么深,不参与最优价和近价件数" : "");
    return `<tr class="${cliff ? "cliff" : ""}${l.stale ? " stale" : ""}" title="${esc(t)}">
      <td class="p">${num(l.price)}<i>${offTxt}</i></td>
      <td class="qt"><span class="qbar ${kind}" style="width:${w}%"></span><b>${num(l.qty)}</b></td>
      <td class="c">${Number(l.orders)}</td>
      <td class="c">${num(l.cum_qty)}</td>
      <td class="t">${spanText(l.standing_hours)}</td>
    </tr>`;
  }).join("");
}

// 成交量柱 + 均价折线,叠在一张图上 —— 跟游戏里「市场历史」同一个意思。
// x 按真实日期排,缺数据的日子留出空档,不会被挤成等距
function tradeChart(series, W = 420) {
  const pts = (series || []).filter(p => p[1] > 0 && p[2] > 0);
  if (pts.length < 2) return `<div class="void">成交数据不足(${pts.length} 天)</div>`;
  const H = 150, pad = 26, padB = 30;
  const xs = pts.map(p => Date.parse(p[0] + "T00:00:00Z"));
  const x0 = Math.min(...xs), x1 = Math.max(...xs);
  const qMax = Math.max(...pts.map(p => p[1])) || 1;
  const ps = pts.map(p => p[2]);
  const pMin = Math.min(...ps), pMax = Math.max(...ps);
  const base = H - padB, top = pad;
  const sx = x => x1 === x0 ? W / 2 : pad + (x - x0) / (x1 - x0) * (W - 2 * pad);
  const sy = p => pMax === pMin ? (base + top) / 2 : base - (p - pMin) / (pMax - pMin) * (base - top);
  const days = Math.max(1, Math.round((x1 - x0) / 86400000) + 1);
  const bw = Math.max(2, (W - 2 * pad) / days * 0.6);

  const bars = pts.map((p, i) => {
    const h = p[1] / qMax * (base - top);
    return `<rect x="${(sx(xs[i]) - bw / 2).toFixed(1)}" y="${(base - h).toFixed(1)}"
      width="${bw.toFixed(1)}" height="${h.toFixed(1)}" class="vbar"><title>${esc(p[0])}
成交 ${num(p[1])} 件
均价 ${num(p[2])}</title></rect>`;
  }).join("");
  const line = pts.map((p, i) => `${i ? "L" : "M"}${sx(xs[i]).toFixed(1)},${sy(p[2]).toFixed(1)}`).join("");
  const dots = pts.map((p, i) => `<circle cx="${sx(xs[i]).toFixed(1)}" cy="${sy(p[2]).toFixed(1)}" r="2.4"
      class="vdot"><title>${esc(p[0])} · 均价 ${num(p[2])} · 成交 ${num(p[1])} 件</title></circle>`).join("");
  return `<svg viewBox="0 0 ${W} ${H}" class="tradechart" role="img" aria-label="成交量与均价走势">
    ${bars}<path d="${line}" class="vline"/>${dots}
    <text x="2" y="12" class="ax">${num(pMax)}</text>
    <text x="2" y="${base + 11}" class="ax">${num(pMin)}</text>
    <text x="${W - 2}" y="12" class="ax" text-anchor="end">柱=成交量 峰值 ${num(qMax)} 件</text>
    <text x="${pad}" y="${H - 4}" class="ax">${esc(pts[0][0].slice(5))}</text>
    <text x="${W - pad}" y="${H - 4}" class="ax" text-anchor="end">${esc(pts[pts.length - 1][0].slice(5))}</text>
  </svg>`;
}

function renderLadder() {
  const d = ladder.data;
  if (!d) return;
  const { city, quality } = ladder;
  // 价格和成交数据取矩阵里同一格:卡片上写什么,这里就是什么
  const g = lookup.data && lookup.itemId === ladder.itemId ? lookup.data : null;
  const cell = g ? lookupCell(g, city, quality) : null;
  const P = g?.params || {};
  const cs = cell?.sell || {}, cb = cell?.buy || {}, sp = cell?.spread, h = cell?.history;
  const sell = d.sell, buy = d.buy;
  const minBid = d.min_bid_depth ?? P.min_bid_depth ?? 20;

  // 比例条按最优价 20% 以内那几档定刻度,不然一张 80 万件的 1 银占位单把其他条全压没
  const nearQty = [...sell.levels, ...buy.levels].filter(l => !l.stale && l.off <= 0.2).map(l => l.qty);
  const cap = Math.max(1, ...(nearQty.length ? nearQty : [...sell.levels, ...buy.levels].map(l => l.qty)));

  const stat = (k, v, hint) => `<div class="bstat" title="${esc(hint || "")}"><b>${v}</b><span>${k}</span></div>`;
  const srcOf = s => s.pick === "capture" ? `来源:抓包,${ageText(s.age_hours)} 前`
    : s.pick === "aodp" ? `来源:AODP,${s.age_hours == null ? "没有时间戳" : ageText(s.age_hours) + " 前"}` : "";
  // 深度闸门对两侧的判定,和格子上"闸门"那一行、机会页卡片同一份
  const depthH = P.depth_max_hours ?? 2;
  const gateVal = s => s.depth ? `${num(s.depth.qty_near)}${s.depth.truncated ? "+" : ""} 件`
    : s.note ? "未参与" : s.pick === "aodp" ? "不判" : "—";
  const gateTip = (s, word) => s.depth || s.note ? `${word}深度闸门:${gateText(s, depthH)}`
    : s.pick === "aodp" ? `${word}的价来自 AODP,不给件数,深度闸门对这一侧不生效` : "";
  const stats = [
    stat("最低卖价", cs.best ? num(cs.best) : "—", `别人挂着卖的最低价。${srcOf(cs)}`),
    stat("最高买价", cb.best ? num(cb.best) : "—", `别人挂着收的最高价。${srcOf(cb)}`),
    stat("卖方闸门近价", gateVal(cs), gateTip(cs, "卖方")),
    stat("买方闸门近价", gateVal(cb), gateTip(cb, "买方")),
    stat("买卖价差", sp ? pct(sp.raw) : "—", "未扣税费的原始价差"),
    stat("税后毛利", sp ? pct(sp.margin) : "—",
      `挂买收货再挂卖出货,扣掉 ${pct(P.friction ?? 0.09)} 摩擦后的净毛利率;真实盈亏平衡价差 ${pct(P.breakeven ?? 0.0963, 2)}`),
    stat("挂买→挂卖", sp ? `${num(sp.my_bid)}→${num(sp.my_ask)}` : "—", "进游戏照抄的两个数(各让 1 银抢队首)"),
    stat("单件净赚", sp ? num(sp.profit_per_unit) : "—", "税后"),
    stat("7日 / 30日均价", h ? `${h.avg_7d ? num(h.avg_7d) : "—"} / ${num(h.avg_30d)}` : "—",
      "成交量加权,不含今天。零星成交日和万笔成交日不等权"),
    stat("成交区间", h?.price_min ? `${num(h.price_min)}~${num(h.price_max)}` : "—", "30 日窗口里各天均价的最低 ~ 最高"),
    // trend_30d 是小数(0.05 = 涨 5%),和销量榜的 trend_pct(百分数)口径不同,这里按小数显示
    stat("30日趋势", h?.trend_30d != null && h.days_30d >= 2 ? `${h.trend_30d >= 0 ? "+" : ""}${pct(h.trend_30d)}` : "—",
      h ? `30 日各天均价的最小二乘趋势,拟合度 R² ${h.trend_fit != null ? h.trend_fit.toFixed(2) : "—"}` : ""),
    stat("价格波动", h?.cv ? h.cv.toFixed(2) : "—", h ? `30 日价格变异系数(标准差 ${num(h.stddev_30d)} / 均价)` : ""),
    stat(h && !h.days_7d ? "日均成交(30日)" : "日均成交",
      h ? num(h.days_7d ? h.daily_qty_7d : h.daily_qty_30d) + " 件" : "—",
      h ? `7 日窗口 ${h.days_7d} 天有数据;30 日口径 ${num(h.daily_qty_30d)} 件/天` : ""),
    stat(h && !h.days_7d ? "日均流水(30日)" : "日均流水",
      h ? silver(h.days_7d ? h.daily_silver_7d : h.daily_qty_30d * h.avg_30d) : "—",
      h && !h.days_7d ? "7 日内没有成交数据,按 30 日日均件数 × 30 日均价" : "7 日日均件数 × 7 日均价"),
  ].join("");

  // 吃单量:沿着最近一轮的阶梯吃到这么多件的真实均价。AODP 给不了这个
  const fillTxt = (f, verb) => !f || !f.got ? `${verb}:没有挂单`
    : `${verb}均价 <b>${num(f.vwap)}</b>(滑点 ${pct(f.slippage)}${f.filled ? "" : `,只够 ${num(f.got)} 件`})`;
  const fillRow = `<div class="fillrow" title="沿着最近一轮的阶梯吃到这么多件的成交量加权均价;灰掉的旧档不算">
    <label>吃单量 <input type="number" id="ld-qty" min="1" step="100" value="${ladderQty()}"> 件</label>
    <span>${fillTxt(sell.fill, "买入")}</span><span>${fillTxt(buy.fill, "卖出")}</span>
  </div>`;

  const notes = [];
  if (city === "Black Market") {
    notes.push(`<div class="diagnosis">黑市只收不卖,卖侧天然是空的。黑市的挂单目前抓不进来
      (那个地点 id 还没核实,服务端故意没并进任何城市),这里只有 AODP 的价。</div>`);
  } else if (!sell.levels.length && !buy.levels.length) {
    notes.push(`<div class="diagnosis warn"><b>最近 ${d.window_hours} 小时里这一格没抓到挂单。</b>
      上面的价格来自 AODP,没有件数。进游戏在这座城的市场<b>点进物品详情页</b>
      (「出售订单 / 购入订单」并排那个界面),两侧就都抓到了。</div>`);
  } else if (!buy.levels.length) {
    notes.push(`<div class="diagnosis warn"><b>买单这一侧一档都没抓到,不等于没人挂买单。</b>
      游戏里的「从集市购买」<b>列表页只发卖单请求</b>,抓不到买单。要拿到买方深度,得<b>点进物品详情页</b>
      —— 就是「出售订单 / 购入订单」并排那个界面,进去一次两侧就都抓到了。
      ${sell.age_hours != null ? `这座城的卖单是 ${ageText(sell.age_hours)} 前抓的。` : ""}</div>`);
  } else if (!sell.levels.length) {
    notes.push(`<div class="diagnosis warn"><b>卖单这一侧没抓到。</b>最近一次只看到了买单,
      进物品详情页翻一下「出售订单」就有了。</div>`);
  }
  // 成交价落在买卖价之间哪个位置 —— 贴着卖价说明成交都是"别人按卖价买走",
  // 挂买单收不到货;落在中间说明两侧都在成交
  const avg = h ? (h.avg_7d || h.avg_30d) : 0;
  if (avg && cb.best && cs.best && cs.best > cb.best) {
    const pos = (avg - cb.best) / (cs.best - cb.best);
    const thinBid = buy.levels.length && buy.support.qty_near < minBid;
    const where = pos < 0 ? "比最高买价还低" : pos > 1 ? "比最低卖价还高" : `落在买卖价之间的 ${(pos * 100).toFixed(0)}% 处`;
    notes.push(`<div class="diagnosis"><b>成交均价${where}。</b>
      ${pos > 0.8 ? `几乎所有成交都发生在<b>卖价</b>一侧 —— 货是被人按卖价买走的,没人肯砸到买价上。
          你挂买单进去大概率一直挂着,而 2.5% 创建费下单就扣、不退。`
        : pos < 0.2 ? `成交集中在<b>买价</b>一侧 —— 买单容易成交,难的是把货挂出去。`
        : `买卖两侧都在成交,双挂(挂买收货 + 挂卖出货)在这个物品上说得通。`}
      ${thinBid ? `<br>而且最高买价 ${pct(P.near_pct ?? 0.05, 0)} 以内只有 ${num(buy.support.qty_near)} 件在收。` : ""}
      <br><span class="sub">均价是 ${h.avg_7d ? 7 : 30} 日成交量加权均价(${h.source === "capture" ? "自抓" : "AODP"})。
      AODP 的成交只统计卖单,天生偏向卖价一侧。</span></div>`);
  }
  const dropped = sell.dropped_orders + buy.dropped_orders;
  const staleN = sell.stale_orders + buy.stale_orders;
  const prevN = (sell.prev_page_orders || 0) + (buy.prev_page_orders || 0);
  if (dropped || staleN || prevN) {
    notes.push(`<div class="diagnosis">${dropped ? `已剔除 ${Number(dropped)} 张上一轮翻到、这一轮没再出现的单(多半已成交或撤单)。` : ""}
      ${staleN ? `灰掉的 ${Number(staleN)} 张是上一轮翻得更深才看到的,这一轮没翻到那么深,不参与最优价和近价件数。` : ""}
      ${prevN ? `最近一次只翻到了后面的页:前面 ${Number(prevN)} 张单是更早那一页看到的,照样算在盘口里
        (那几档的「挂了多久」悬停能看到最后一次看到是多久前)。进游戏重新点开一次就全是新的了。` : ""}</div>`);
  }

  const foot = (s, kind) => `这一侧 ${s.level_count} 档 · ${s.orders} 单 · 共 ${num(s.qty_total)} 件` +
    (s.truncated ? ` · <span title="游戏一页最多 ${d.page_size} 单,只看到了翻到的那几页">可能没翻完,总数是下界</span>` : "") +
    (kind === "buy" && s.qty_total ? ` <span class="warnish">(含 1 银那种占位单,别拿总数当深度)</span>` : "");
  const histSrc = !h ? "" : h.source === "capture" ? "自抓" : h.stored ? "库里存下的 AODP(这次没取到)" : "AODP";

  // 走势图按右栏的内容宽度画,SVG 里 1 个单位就是 1px,坐标字不会被缩成 6px。
  // 画完再量一次:内容一长右栏就出竖向滚动条,宽度会少十几 px(fitLookup 里重画)
  const bp = $("bookpanel"), bcs = getComputedStyle(bp);
  const chartW = Math.max(300, Math.floor(bp.clientWidth - parseFloat(bcs.paddingLeft) - parseFloat(bcs.paddingRight)));
  ladder.series = h?.series || [];

  // 面板顶部那几个数来自价格表,下面的阶梯来自挂单簿接口。正常情况两份是一起拉的;
  // 表没拉到(或还在路上)时两份就差了一段时间,得说出来,不能让人照抄旧价
  const gap = g ? (Date.parse(d.fetched_at) - Date.parse(g.fetched_at)) / 60000 : 0;
  const gapText = m => m < 1 ? `${Math.max(1, Math.round(m * 60))} 秒` : `${Math.round(m)} 分钟`;
  const staleHint = ladder.gridLate
    ? `<span class="stalehint">价格表还在重拉(AODP 排队中),上面几个价暂时是 ${gapText(Math.max(0, gap))}前查的。</span>`
    : Math.abs(gap) > 1
      ? `<span class="stalehint">上面几个价(来自价格表)和下面的阶梯是隔了 ${gapText(Math.abs(gap))}分别拉的,可能对不上。<button class="mini" data-reload>一起刷新</button></span>`
      : "";

  $("bookpanel").innerHTML = `<div class="bookwrap">
    <div class="bookhead">
      <b>${cityMark(city)} · <span class="q" style="--q:var(--q${quality})">${QUALITY[quality] || quality}</span> 完整挂单簿</b>
      <span class="spacer"></span>
      <button class="mini" data-close>收起</button>
      <span class="bsub">${g ? esc(displayName(g.item)) + " · " : ""}自建抓包 · 卖 ${Number(sell.level_count)} 档 / 买 ${Number(buy.level_count)} 档 · 最近 ${Number(d.window_hours)} 小时</span>
      ${staleHint}
    </div>
    <div class="bstats">${stats}</div>
    ${fillRow}
    ${notes.join("")}
    <div class="ladders">
      <div>
        <h4 class="sellh">出售订单 <em>别人挂着卖的 · 从低到高</em></h4>
        <table class="ladder"><thead><tr>
          <th>价格</th><th>件数</th><th>单数</th><th>累计</th><th>挂了多久</th>
        </tr></thead><tbody>${ladderRows(sell.levels, "sell", cap)}</tbody></table>
        <div class="ladderfoot">${foot(sell, "sell")}</div>
      </div>
      <div>
        <h4 class="buyh">购入订单 <em>别人挂着收的 · 从高到低</em></h4>
        <table class="ladder"><thead><tr>
          <th>价格</th><th>件数</th><th>单数</th><th>累计</th><th>挂了多久</th>
        </tr></thead><tbody>${ladderRows(buy.levels, "buy", cap)}</tbody></table>
        <div class="ladderfoot">${foot(buy, "buy")}</div>
      </div>
    </div>
    <h4 class="chh">成交走势 <em>柱=成交件数,线=当天成交量加权均价</em></h4>
    <div class="chartbox">${tradeChart(h?.series, chartW)}</div>
    <div class="ladderfoot">${h
      ? `日线,不含今天 · 30 天里 ${h.days_30d} 天有数据 · 来源 ${histSrc}。和左边格子里的 7 日 / 30 日均价是同一份数据`
      : "这一格没有成交历史。"}
      <br>旧版画的是抓包的 6 小时桶;公会版服务端还不收抓包的成交历史,暂时只有 AODP 日线。</div>
  </div>`;
  fitLookup();
}

// ── 三级级联菜单:大类 → 子类 → 物品族 ─────────────────────
let menuTree = null;

const menuCat = () => (menuTree || []).find(c => c.id === lookup.sel.cat);
const menuSub = () => (menuCat()?.subs || []).find(s => s.id === lookup.sel.sub);
const menuFam = () => (menuSub()?.families || []).find(f => f.key === lookup.sel.fam);
function menuPath() {
  const parts = [menuCat()?.label, menuSub()?.label, menuFam()?.label].filter(Boolean);
  return parts.length ? parts.join(" › ") : "全部物品";
}
const catCol = () => menuTree.map(c => ({ id: c.id, label: c.label, count: c.count, node: c }));
const subCol = cat => cat.subs.map(s => ({ id: s.id, label: s.label, count: s.count, node: s }));
const famCol = sub => sub.families.map(f => ({ id: f.key, label: f.label, count: f.count, node: f }));

// 打开时照当前选中的路径展开,选过的东西一眼看得见
async function openMenu() {
  if (!menuTree) {
    try { menuTree = await getJSON("/api/menu"); } catch (e) {
      $("list-head").textContent = String(e.message || e);
      return;
    }
  }
  const cols = [catCol()], trail = [];
  const cat = menuCat(), sub = menuSub();
  if (cat) { trail.push(cat.id); cols.push(subCol(cat)); }
  if (cat && sub) { trail.push(sub.id); cols.push(famCol(sub)); }
  if (cat && sub && menuFam()) trail.push(lookup.sel.fam);
  renderMenu(cols);
  trail.forEach((id, d) => $("menu").querySelector(`li[data-depth="${d}"][data-id="${CSS.escape(id)}"]`)
    ?.setAttribute("aria-selected", "true"));
  $("menu").classList.remove("bare");
  $("menu-btn").setAttribute("aria-expanded", "true");
}
function closeMenu() {
  $("menu").classList.add("bare");
  $("menu-btn").setAttribute("aria-expanded", "false");
}
$("menu-btn").addEventListener("click", () => {
  if ($("menu").classList.contains("bare")) openMenu(); else closeMenu();
});
document.addEventListener("click", e => { if (!e.target.closest(".picker")) closeMenu(); });
document.addEventListener("keydown", e => { if (e.key === "Escape") closeMenu(); });

// 和旧版一样固定三栏并排,每栏顶上一行列标题;还没展开的栏是空的 <ul>,
// CSS 的 :empty 给它写"← 指向左边一栏"。列标题不带 data-depth,悬停和点击都认不到它
const MENU_HEADS = ["分类", "子类", "物品"];
function renderMenu(columns) {
  $("menu").innerHTML = MENU_HEADS.map((head, depth) => {
    const col = columns[depth];
    if (!col) return `<ul></ul>`;
    return `<ul><li class="menu-head" role="presentation">${head}</li>` + col.map(x =>
      `<li data-depth="${depth}" data-id="${esc(x.id)}"${depth < 2 ? ' class="has"' : ""}><b>${esc(x.label)}</b><span>${Number(x.count) || 0}</span></li>`
    ).join("") + `</ul>`;
  }).join("");
  $("menu")._columns = columns;
}

// 悬停某一项就把它的下一层追加到右边,和游戏里市场那几个下拉一样
$("menu").addEventListener("mouseover", e => {
  const li = e.target.closest("li[data-depth]");
  if (!li) return;
  // 已经选中的不重绘。重绘会把鼠标底下的元素整个换掉,从文字挪到右边计数
  // 那一下就会触发;按下和松开落在两个不同的元素上,浏览器不派发 click ——
  // 最内层物品族"点了没反应"就是这么来的
  if (li.hasAttribute("aria-selected")) return;
  const depth = +li.dataset.depth;
  if (depth === 2) {   // 最内层没有下一级,只挪高亮
    li.parentElement.querySelector("[aria-selected]")?.removeAttribute("aria-selected");
    li.setAttribute("aria-selected", "true");
    return;
  }
  const cols = $("menu")._columns;
  const picked = cols[depth].find(x => x.id === li.dataset.id);
  const kept = cols.slice(0, depth + 1);

  if (depth === 0) {
    kept.push(picked.node.subs.map(s => ({ id: s.id, label: s.label, count: s.count, node: s })));
  } else if (depth === 1) {
    kept.push(picked.node.families.map(f => ({ id: f.key, label: f.label, count: f.count, node: f })));
  }
  const trail = [];
  for (let d = 0; d < depth; d++)
    trail.push($("menu").querySelector(`li[data-depth="${d}"][aria-selected]`)?.dataset.id);
  trail.push(li.dataset.id);

  renderMenu(kept);
  // 重绘后高亮丢了,照着刚才记下的路径补回来
  trail.forEach((id, d) => {
    if (id) $("menu").querySelector(`li[data-depth="${d}"][data-id="${CSS.escape(id)}"]`)
      ?.setAttribute("aria-selected", "true");
  });
});

// 三层都能点:点大类就列整个大类,点物品族就只列那一族。悬停只展开下一栏,
// 不动筛选 —— 手划过去不该把左栏刷掉
$("menu").addEventListener("click", e => {
  const li = e.target.closest("li[data-depth]");
  if (!li) return;
  const depth = +li.dataset.depth;
  const path = [];
  for (let d = 0; d < depth; d++)
    path.push($("menu").querySelector(`li[data-depth="${d}"][aria-selected]`)?.dataset.id || "");
  path.push(li.dataset.id);
  lookup.sel = { cat: path[0] || "", sub: path[1] || "", fam: path[2] || "" };
  closeMenu();
  $("search").value = "";
  refreshList();
});

// ── 销量榜 ─────────────────────────────────────────────────
let rankRows = [];
// 初值和表头上 data-dir="desc" 的那一列一致,也就是服务端默认的排序
const rankSort = { key: "daily_silver", dir: "desc" };

function sparkline(series) {
  if (!series || series.length < 2) return "";
  const ys = series.map(p => p[1]);
  const lo = Math.min(...ys), hi = Math.max(...ys), span = hi - lo || 1;
  const w = 76, h = 20;
  const pts = series.map((p, i) =>
    `${((i / (series.length - 1)) * w).toFixed(1)},${(h - ((p[1] - lo) / span) * h).toFixed(1)}`).join(" ");
  return `<svg class="spark" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" aria-hidden="true">
    <polyline points="${pts}" fill="none" stroke="currentColor" stroke-width="1.5"
              stroke-linejoin="round" vector-effect="non-scaling-stroke"/></svg>`;
}

// 走势线的颜色:涨绿跌红,震荡青铜,平盘淡灰(旧版就是这么配的)
const TREND_CLASS = { up: "pos", down: "neg", choppy: "medium", flat: "sub" };

function renderRank(rows) {
  $("rank-empty").style.display = rows.length ? "none" : "";
  for (const r of rows) noteName(r.item_id, r.item_name);
  const fix = (v, d) => (v == null || !isFinite(v) ? "—" : v.toFixed(d));
  $("rank-rows").innerHTML = rows.map(r => `<tr>
    <td class="l">${itemCell(r.item_id, r.item_name)}</td>
    <td class="l">${r.city === "全服" ? "全服" : cityDot(r.city)}</td>
    <td title="窗口内共成交 ${num(r.total_qty)} 件">${num(r.daily_qty)}</td>
    <td><b>${num(r.daily_silver)}</b></td>
    <td>${num(r.avg_price)}</td>
    <td>${num(r.price_min)}</td>
    <td>${num(r.price_median)}</td>
    <td>${num(r.price_max)}</td>
    <td>${fix(r.volatility, 2)}</td>
    <td class="${r.trend === "up" ? "pos" : r.trend === "down" ? "neg" : ""}"
        title="窗口内的最小二乘涨跌幅(%),拟合度 R² ${fix(r.trend_fit, 2)}">${esc(TREND[r.trend] || "")} ${r.trend_pct >= 0 ? "+" : ""}${fix(r.trend_pct, 1)}%</td>
    <td class="l ${TREND_CLASS[r.trend] || ""}">${sparkline(r.series)}</td>
    <td class="sub" title="${r.last_day ? "最近一天有成交:" + esc(r.last_day) : ""}">${Number(r.days_with_data) || 0} 天</td>
  </tr>`).join("");
}

async function loadRank() {
  const q = new URLSearchParams({
    city: $("r-city").value || "__all__", window: $("r-window").value,
    quality: $("r-quality").value, limit: "60",
  });
  if ($("r-category").value) q.set("category", $("r-category").value);
  try {
    rankRows = await getJSON("/api/rank?" + q);
    renderRank(sortBy(rankRows, rankSort.key, rankSort.dir));
    $("rank-meta").textContent = `${rankRows.length} 条`;
  } catch (e) {
    $("rank-empty").textContent = String(e.message || e);
    $("rank-empty").style.display = "";
  }
}
for (const id of ["r-city", "r-window", "r-quality", "r-category"])
  $(id).addEventListener("change", loadRank);
sortable("#v-rank thead", rankSort, () => rankRows, renderRank);

// ── 实时:一条 WS 连接,各页登记自己要订阅的 key ─────────────────
// 协议(服务端 hub/topic.go):
//   → {"op":"sub"|"unsub","keys":[...],"topics":["scan"]}   key = item|city|quality|side(0 卖单 1 买单)
//   ← 报价 {k,p,d,n,t},没有 type 字段;订阅了 scan 的连接,服务端每发布一次扫描结果
//     (AODP 全量或抓包快速重算)都收到 {"type":"scan","evaluated_at","started_at","digest",…,"full","ingest"}
//   ← {"type":"resync"}:这条连接积压丢过消息,不用订阅(见 rtResync)
// 老服务端不认识 topics:encoding/json 直接忽略未知字段,所以现在就带上是安全的
const rt = {
  ws: null, up: false, backoff: 500, openedAt: 0, opens: 0,   // opens:第几次连上,>1 就是重连
  owners: new Map(),     // 登记方 → { keys: Set, snapshot: bool }("live" 实时页、"lookup" 查价页)
  subbed: new Set(),     // 这条连接上已经订上的 key
  quoteFns: [], scanFns: [], openFns: [],
  resyncTimer: 0, resyncs: 0,   // 服务端 resync 通知的合并定时器、处理过几次(见 rtResync)
};
const SUB_CHUNK = 400;   // 服务端一条指令最多读 64KB,一个 key 四十来字节
const SNAP_CHUNK = 100;  // /api/quotes 一次带多少个 key

function rtUnion() {
  const u = new Set();
  for (const o of rt.owners.values()) for (const k of o.keys) u.add(k);
  return u;
}
function rtSend(op, keys, topics) {
  if (!rt.up || rt.ws?.readyState !== WebSocket.OPEN) return;
  if (!keys.length && !topics) return;
  for (let i = 0; i === 0 || i < keys.length; i += SUB_CHUNK) {
    const msg = { op, keys: keys.slice(i, i + SUB_CHUNK) };
    if (topics && i === 0) msg.topics = topics;
    rt.ws.send(JSON.stringify(msg));
  }
}
// 登记方换一整套 key:和这条连接上已订的求差集,只发增减的那部分。
// snapshot:新加进来的 key 要不要立刻拉一次快照(实时页要;查价页自己会拉 grid,不要)
function rtSetKeys(owner, keys, opts = {}) {
  const next = new Set(keys);
  const prev = rt.owners.get(owner)?.keys || new Set();
  rt.owners.set(owner, { keys: next, snapshot: opts.snapshot !== false });
  if (!rt.up) return;   // 连上时 onopen 按并集整体订阅,这里不用管
  const want = rtUnion();
  const add = [...want].filter(k => !rt.subbed.has(k));
  const drop = [...rt.subbed].filter(k => !want.has(k));
  rtSend("unsub", drop);
  rtSend("sub", add);
  rt.subbed = want;
  if (opts.snapshot !== false) rtSnapshot([...next].filter(k => !prev.has(k)));
}
// 返回哪些 key 服务端给了报价、哪些所在的那一批没拉到。没给报价 = 服务端那边这一边
// 已经空了(或超出新鲜窗口),实时页据此把它从表上拿掉 —— 盘口清空时服务端不推任何消息
async function rtSnapshot(keys) {
  const got = new Set(), failed = new Set();
  for (let i = 0; i < keys.length; i += SNAP_CHUNK) {
    const part = keys.slice(i, i + SNAP_CHUNK);
    try {
      const qs = await getJSON("/api/quotes?" + new URLSearchParams({ keys: part.join(",") }));
      for (const q of qs || []) { if (typeof q.k === "string") { got.add(q.k); rtQuote(q, false); } }
    } catch (e) {
      for (const k of part) failed.add(k);   // 服务端抖一下:下一次推送或重连会补上
    }
  }
  return { keys, got, failed };
}
rt.quoteFns.push(onLookupQuote);
// 查价页重连之后补一次:断线期间这件物品的挂单可能变了,推送是收不到了的。
// 它订阅时不要报价快照(表自己就是整张拉的),onopen 里的快照补不到它,得在这里补。
// 第一次连上不用:那时表刚拉过
rt.openFns.push(() => { if (rt.opens > 1 && lookup.data) lookupLiveKick(); });
function rtQuote(q, pushed) {
  for (const fn of rt.quoteFns) {
    try { fn(q, pushed); } catch (e) { console.error("处理报价出错", e); }
  }
}
function rtConnect() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  let ws;
  try { ws = new WebSocket(`${proto}//${location.host}/ws`); } catch (e) { rtRetry(); return; }
  rt.ws = ws;
  ws.onopen = () => {
    rt.up = true; rt.backoff = 500; rt.openedAt = Date.now(); rt.opens++;
    renderConn();
    // ① 重新订阅 keys 和 topics:新连接(可能是另一个实例)完全不知道你要看什么
    const keys = [...rtUnion()];
    rt.subbed = new Set(keys);
    rtSend("sub", keys, ["scan"]);
    // ② 补快照:断线期间错过的变化。少了这步界面会停在旧值上,
    //    却看起来"一切正常"——这是这类系统最常见的 bug。实时页顺便拿它剔掉已经空了的边
    const snap = new Set();
    for (const o of rt.owners.values()) if (o.snapshot) for (const k of o.keys) snap.add(k);
    if (snap.size) liveResync([...snap]);
    for (const fn of rt.openFns) {
      try { fn(); } catch (e) { console.error(e); }
    }
  };
  // 按类型分派。以前每条都当报价处理,服务端一推 {type:"scan"} 就在 q.k.split 上抛异常
  ws.onmessage = e => {
    let msg;
    try { msg = JSON.parse(e.data); } catch (err) { return; }
    if (!msg || typeof msg !== "object") return;
    if (msg.type === "scan") {
      for (const fn of rt.scanFns) {
        try { fn(msg); } catch (err) { console.error("处理扫描通知出错", err); }
      }
      return;
    }
    if (msg.type === "resync") { rtResync(); return; }
    if (msg.type) return;   // 以后新加的主题,不认识就不管
    if (typeof msg.k === "string") rtQuote(msg, true);
  };
  ws.onclose = () => {
    if (rt.ws !== ws) return;
    rt.up = false;
    rt.subbed = new Set();
    renderConn();
    rtRetry();
  };
}
function rtRetry() {
  setTimeout(rtConnect, rt.backoff);
  rt.backoff = Math.min(rt.backoff * 2, 30000);   // 指数退避
}

// 服务端说这条连接积压丢过消息({"type":"resync"},见 hub.ResyncMessage)。丢的是哪几条不知道,
// 被丢的盘口要是之后没人再翻,就一直停在旧值上 —— 以前只能等实时页每分钟一次的整体对账,
// 而且实时页不在前台时那个对账根本不跑。这里:要快照的 key 全部回拉一次,查价页重拉一次表,
// 再重订 scan 拿服务端补发的最近一条通知(摘要没变就不重拉 /api/scan)。连着来几条只做一次
function rtResync() {
  if (rt.resyncTimer) return;
  rt.resyncTimer = setTimeout(() => {
    rt.resyncTimer = 0;
    rt.resyncs++;
    rtSend("sub", [], ["scan"]);
    const snap = new Set();
    for (const o of rt.owners.values()) if (o.snapshot) for (const k of o.keys) snap.add(k);
    if (snap.size) liveResync([...snap]);
    if (lookup.data) lookupLiveKick();
  }, 300);
}

// 顶栏「推送」那一格
function renderConn() {
  $("conn").innerHTML = rt.up ? '<span class="dot live"></span>已连接' : '<span class="dot dead"></span>重连中…';
  $("conn-sub").textContent = sync.poll ? "推送 · 扫描结果靠轮询" : "推送";
  $("conn-sub").title = sync.poll
    ? "这两分钟没收到服务端的扫描通知(服务端版本较旧,或连接断过),每分钟拉一次 /api/scan 比对" : "";
}

// ── 扫描结果的实时同步 ──
// 订阅 scan 主题:摘要(digest)变了才重拉 /api/scan。连上后 2 分钟内一条 scan 消息都没收到
// (老服务端没有这个主题,或者 hub 积压丢了)就退回每 60 秒拉一次、比较 evaluated_at || started_at;
// 一收到 scan 消息就停掉轮询。看门狗是滚动的:任何时候 2 分钟没消息都会退回轮询
const sync = {
  watchdogMs: 120000, pollMs: 60000,   // 做成字段:排查时能在控制台临时调小
  lastMsgAt: 0, bootAt: Date.now(),
  digest: "", stamp: "", evalAt: "", fullAt: "",
  poll: null, busy: false, again: false, want: "", pulledAt: 0,
};
const scanStamp = r => (r && (r.evaluated_at || r.started_at)) || "";
// 两个时间戳取后一个。推送、轮询、重新扫描各自带着时间戳到,先后不保证
const later = (a, b) => (!a ? b || "" : !b ? a : Date.parse(b) >= Date.parse(a) ? b : a);

async function pullScan(why) {
  if (sync.busy) { sync.again = true; return; }
  sync.busy = true;
  try {
    const res = await getJSON("/api/scan");
    sync.pulledAt = Date.now();
    clearTimeout(sync.retry);   // 上一次失败排的重试不用了:这次已经拉到(推送先到时常见)
    // 同一份内容:数据龄本来就按本地时间在走,不用重画,只记下最新的重算时刻。
    // 摘要覆盖整份 /api/scan(服务端 scan.Digest),只剔 evaluated_at 和随时间自己变的那几样
    if (sameResult(res)) noteEval(res.evaluated_at, res.started_at);
    else applyResult(res, why);
  } catch (e) {
    clearTimeout(sync.retry);
    if (!scan) {
      // 503 = 服务端还没扫完第一轮,不是错误。第一轮通常一两分钟就好,
      // 这时不等看门狗的 2 分钟,15 秒后自己再问一次(推送到了也会提前拉)
      renderIdeasEmpty(e);
      sync.retry = setTimeout(() => { if (!scan) pullScan("retry"); }, 15000);
    } else if (why !== "retry") {
      // 手上有一份,这次没拉到(重连时新服务端还在跑第一轮、网络抖了一下):15 秒后再试一次。
      // 只试一次,再失败就等推送或看门狗,服务端长时间挂着时不会每 15 秒打一次
      sync.retry = setTimeout(() => pullScan("retry"), 15000);
    }
  } finally {
    sync.busy = false;
    if (sync.again) {
      const want = sync.want;
      sync.again = false;
      sync.want = "";
      if (!(want && want === sync.digest)) pullScan("again");
    }
  }
}

function onScanPush(msg) {
  sync.lastMsgAt = Date.now();
  stopPoll();
  noteEval(msg.evaluated_at, msg.started_at);
  noteIngest(msg.ingest);   // 串城横幅跟着通知走,摘要没变也要看
  if (scan && msg.digest && msg.digest === sync.digest) return;   // 摘要没变,不重拉
  if (sync.busy) { sync.again = true; sync.want = msg.digest || ""; return; }
  pullScan("push");
}
rt.scanFns.push(onScanPush);

// 重连后补一次 /api/scan 快照:断线那段时间的推送收不到了。刚拉过就不重复拉
rt.openFns.push(() => {
  if (!sync.busy && Date.now() - sync.pulledAt > 5000) pullScan("reconnect");
});

// 顶栏的重算时间、页脚和总览概况里的「最近重算」都跟着最新的 evaluated_at 走:
// 摘要没变时手上那份结果不换,它自己的 evaluated_at 就停住了
function noteEval(evaluatedAt, startedAt) {
  sync.evalAt = later(sync.evalAt, evaluatedAt);
  sync.fullAt = later(sync.fullAt, startedAt);
  renderScanAge();
  if (scan) { renderIdeasFoot(scan); renderScanMeta(scan); }
}

function startPoll() {
  if (sync.poll) return;
  sync.poll = setInterval(() => pullScan("poll"), sync.pollMs);
  renderConn();
  pullScan("poll");
}
function stopPoll() {
  if (!sync.poll) return;
  clearInterval(sync.poll);
  sync.poll = null;
  renderConn();
}
setInterval(() => {
  const quiet = Date.now() - Math.max(sync.lastMsgAt, rt.openedAt, sync.bootAt);
  if (!sync.poll && quiet > sync.watchdogMs) startPoll();
}, 5000);

// ── 实时行情页 ─────────────────────────────────────────────
// 看哪些盘口不再写死(以前是 5 个物品 × 4 座城,缺 Fort Sterling / Caerleon / Brecilien):
// 机会板日收益前 LIVE_TOP 名的物品 + 查价页正在看的那件(它的所有品质),
// 在配置里的每座城都订买卖两边。机会板一更新、查价换了物品,订阅跟着换
const LIVE_TOP = 15;
const DEFAULT_CITIES = ["Thetford", "Fort Sterling", "Lymhurst", "Martlock", "Bridgewatch", "Caerleon", "Brecilien"];
// 每一边各存各的:p 价、d 件数、n 单数、t 服务端给的时间(ts 是它的毫秒数)、rx 本机收到的时刻
const liveState = new Map();   // item|city|quality → { sell:{p,d,n,t,ts,rx}, buy:{…} }
const live = { targets: [], cities: [], sig: "", verify: new Set(), verifyTimer: 0 };
// 服务端 -fresh 的默认值:/api/quotes 和推送都只给这么久之内看到过的盘口。
// 超过的边服务端不会推"没了",这里自己按时间拿掉
const LIVE_FRESH_MS = 30 * 60000;
// 推送到了之后隔一会儿对这批 key 回拉一次 /api/quotes,用回拉的结果为准。
// 老服务端的推送发在单子落库之前,推出去的是上一个价;新服务端推的已经是落库后的,回拉只是多一次确认
const LIVE_VERIFY_MS = 2500;
// 实时页开着时每分钟整体对一次:盘口清空(服务端不推消息)靠它收敛。推送被丢(连接积压)
// 新服务端会补发 resync、当场回拉(rtResync),这一路只剩兜老服务端
const LIVE_RESYNC_MS = 60000;

// 城市取扫描覆盖率里的(就是配置里的城市),再并上查价页的城市。Brecilien 总是带上:
// 抓包的 5003 收敛成它,查价页也一直有这一行;配置里没列它时扫描不算它的机会,
// 但成员在那边翻市场时这里照样该看得到
function liveCities() {
  const out = [];
  const add = c => { if (c && c !== "Black Market" && !out.includes(c)) out.push(c); };
  for (const c of scan?.coverage || []) add(c.city);
  for (const c of lookup.data?.cities || []) add(c);
  if (!out.length) DEFAULT_CITIES.forEach(add);
  add("Brecilien");
  return out;
}
function liveTargets() {
  const out = new Map();
  const d = lookup.data;
  if (d?.item?.item_id)
    for (const q of d.qualities?.length ? d.qualities : [1])
      out.set(`${d.item.item_id}|${q}`, { item_id: d.item.item_id, quality: q, name: displayName(d.item), from: "查价" });
  let n = 0;
  for (const r of sortBy(ideas, "daily_profit", "desc")) {
    if (n >= LIVE_TOP) break;
    const k = `${r.item_id}|${r.quality}`;
    if (out.has(k)) continue;
    out.set(k, { item_id: r.item_id, quality: r.quality, name: r.item_name, from: "机会板" });
    n++;
  }
  return [...out.values()];
}
function liveTargetsChanged() {
  const targets = liveTargets(), cities = liveCities();
  const keys = [];
  for (const t of targets) for (const c of cities) for (const s of [0, 1]) keys.push(`${t.item_id}|${c}|${t.quality}|${s}`);
  const sig = keys.join(",");
  live.targets = targets;
  live.cities = cities;
  if (sig === live.sig) return;   // 集合没变(查价页每次实时刷新都会走到这)就什么都不动
  live.sig = sig;
  const want = new Set(keys);
  for (const rk of [...liveState.keys()]) {
    const [i, c, q] = rk.split("|");
    if (!want.has(`${i}|${c}|${q}|0`)) liveState.delete(rk);   // 不再订阅的盘口从表里拿掉
  }
  rtSetKeys("live", keys);
  liveDirty();
}

const agoText = t => {
  const s = (Date.now() - Date.parse(t)) / 1000;
  if (!isFinite(s)) return "";
  return s < 60 ? `${Math.max(0, Math.round(s))} 秒前` : s < 3600 ? `${Math.round(s / 60)} 分钟前` : `${(s / 3600).toFixed(1)} 小时前`;
};
const agoSpan = t => (t ? `<span class="tick-ago" data-t="${esc(t)}">${agoText(t)}</span>` : "");
setInterval(() => { for (const el of document.querySelectorAll(".tick-ago")) el.textContent = agoText(el.dataset.t); }, 15000);

const depthText = v => (v ? `${num(v.d)} 件${v.n ? ` / ${num(v.n)} 单` : ""}` : "");
const rowAt = v => later(v.sell?.t, v.buy?.t);   // 一行的"最近一次看到":两边里新的那个
const keySide = k => {
  const p = k.split("|");
  return { rk: `${p[0]}|${p[1]}|${p[2]}`, side: p[3] === "0" ? "sell" : "buy" };
};

// 画表是攒着画的:同一帧里来的报价只画一次;有新行或者删了边就整表重画一次,
// 其余只改变了的那几格。实时页不在前台时只改 liveState,切过来再画。
// 以前每遇到一个新盘口边就整表 innerHTML 重建一次,重连快照灌 210 条要卡一秒多
const liveUI = { full: true, patches: new Map(), raf: 0, rows: new Map() };
function liveDirty() { liveUI.full = true; liveSchedule(); }
function liveSchedule() {
  if (!liveUI.raf) liveUI.raf = requestAnimationFrame(liveFlush);
}
function liveFlush() {
  liveUI.raf = 0;
  if (currentView !== "live") return;   // 记着的 full / patches 留到 show("live")
  let flashes = [];
  if (!liveUI.full) {
    for (const [pk, dir] of liveUI.patches) {
      const i = pk.indexOf(":"), side = pk.slice(0, i), rk = pk.slice(i + 1);
      const tr = liveUI.rows.get(rk), v = liveState.get(rk);
      if (!tr || !v) { liveUI.full = true; break; }
      const el = liveWrite(tr, v, side);
      if (el && dir) flashes.push([el, dir]);
    }
  }
  if (liveUI.full) {
    liveUI.full = false;
    renderLive();
    flashes = [];
    for (const [pk, dir] of liveUI.patches) {
      const i = pk.indexOf(":"), side = pk.slice(0, i), rk = pk.slice(i + 1);
      const el = dir && liveUI.rows.get(rk)?.querySelector(`[data-f="${side}"]`);
      if (el) flashes.push([el, dir]);
    }
  }
  liveUI.patches.clear();
  renderLiveMeta();
  flashMany(flashes);
}
// 改一行里某一边的那几格,返回价格那一格(要闪的是它)
function liveWrite(tr, v, side) {
  const el = tr.querySelector(`[data-f="${side}"]`);
  if (el) el.textContent = v[side] ? num(v[side].p) : "—";
  const d = tr.querySelector(`[data-d="${side}"]`);
  if (d) d.textContent = depthText(v[side]);
  const at = rowAt(v);
  tr.querySelector("[data-a]").innerHTML = agoSpan(at);
  tr.querySelector("[data-at]").textContent = at ? new Date(at).toLocaleTimeString("zh-CN") : "";
  return el;
}
// 一批元素一起重播闪动:先全部摘掉 class,只强制重排一次,再全部加回去。
// flash() 每个元素各重排一次,一秒几十条推送时就是几十次同步重排
function flashMany(list) {
  if (!list.length) return;
  for (const [el] of list) el.classList.remove("flash-up", "flash-down");
  void document.body.offsetWidth;
  for (const [el, dir] of list) el.classList.add("flash-" + dir);
}

function renderLive() {
  const order = new Map(live.targets.map((t, i) => [`${t.item_id}|${t.quality}`, i]));
  const cityIdx = new Map(live.cities.map((c, i) => [c, i]));
  const rows = [...liveState.entries()].map(([rk, v]) => {
    const [item, city, q] = rk.split("|");
    return { rk, v, item, city, q: Number(q), t: live.targets[order.get(`${item}|${q}`)] };
  }).filter(x => x.t && (x.v.sell || x.v.buy)).sort((a, b) =>
    order.get(`${a.item}|${a.q}`) - order.get(`${b.item}|${b.q}`) || (cityIdx.get(a.city) ?? 99) - (cityIdx.get(b.city) ?? 99));
  const nKeys = live.targets.length * live.cities.length * 2;
  $("live-empty").style.display = rows.length ? "none" : "";
  $("live-empty").textContent = !live.targets.length
    ? "机会板还没有结果,查价页也没选物品 —— 两边有了就自动订阅。"
    : `已订阅 ${nKeys} 个盘口,等待行情…需要有成员开着客户端在游戏里翻这些物品的市场(最近 30 分钟内看到的挂单才算)。`;
  $("live-rows").innerHTML = rows.map(({ rk, v, city, q, t }) => {
    const at = rowAt(v);
    return `<tr data-rk="${esc(rk)}">
      <td class="l">${itemCell(t.item_id, t.name)} <span class="kind flip" title="${t.from === "查价" ? "查价页正在看的物品" : "机会板日收益前几名"}">${t.from}</span></td>
      <td class="l">${qualityTag(q)}</td>
      <td class="l">${cityDot(city)}</td>
      <td><span class="price" data-f="sell">${v.sell ? num(v.sell.p) : "—"}</span></td>
      <td class="depth" data-d="sell">${depthText(v.sell)}</td>
      <td><span class="price" data-f="buy">${v.buy ? num(v.buy.p) : "—"}</span></td>
      <td class="depth" data-d="buy">${depthText(v.buy)}</td>
      <td class="age" data-a>${agoSpan(at)}</td>
      <td class="age" data-at>${at ? new Date(at).toLocaleTimeString("zh-CN") : ""}</td>
    </tr>`;
  }).join("");
  liveUI.rows = new Map([...$("live-rows").children].map(tr => [tr.dataset.rk, tr]));
}

// 说明行。每次落笔都重写(只改格子的时候也是):一行里后到的那一边不触发整表重画,
// 计数不能跟着整表重画才更新
function renderLiveMeta() {
  const order = new Set(live.targets.map(t => `${t.item_id}|${t.quality}`));
  const nKeys = live.targets.length * live.cities.length * 2;
  // 和 nKeys 同一个单位:一边算一个。以前拿行数(物品 × 城)去比按边算的总数
  let seen = 0;
  for (const [rk, v] of liveState) {
    const [item, , q] = rk.split("|");
    if (order.has(`${item}|${q}`)) seen += (v.sell ? 1 : 0) + (v.buy ? 1 : 0);
  }
  const fromLookup = live.targets.filter(t => t.from === "查价").length;
  $("live-meta").textContent = live.targets.length
    ? `订阅了 ${live.targets.length} 个物品(机会板前 ${live.targets.length - fromLookup} 个${fromLookup ? ` + 查价页 ${fromLookup} 个品质` : ""})× ${live.cities.length} 城 × 买卖两边 = ${nKeys} 个盘口(一边算一个),` +
      `其中 ${seen} 个最近 30 分钟有人看到。城市:${live.cities.join("、")}`
    : "";
}

function applyQuote(q, pushed) {
  if (!rt.owners.get("live")?.keys.has(q.k)) return;   // 退订之后还在路上的,或者是查价页订的
  const { rk, side } = keySide(q.k);
  const had = liveState.has(rk);
  const cur = liveState.get(rk) || {};
  const prev = cur[side];
  // 比这一边手上的还旧就丢,推送和快照一视同仁。重连、换订阅时快照和推送同时在路上,
  // 晚到的快照会把新价盖回旧价(hub 的乱序闸门只管推送之间)。一样新的照收
  const ts = Date.parse(q.t);
  if (prev && isFinite(prev.ts) && isFinite(ts) && ts < prev.ts) return;
  // 价没动、件数变了也闪:有人吃掉/补上了最优档
  const dir = prev ? (q.p > prev.p ? "up" : q.p < prev.p ? "down" : q.d !== prev.d ? (q.d > prev.d ? "up" : "down") : "") : "";
  cur[side] = { p: q.p, d: q.d, n: q.n, t: q.t, ts, rx: Date.now() };
  liveState.set(rk, cur);
  if (!had) liveUI.full = true;
  const pk = `${side}:${rk}`;
  liveUI.patches.set(pk, dir || liveUI.patches.get(pk) || "");
  liveSchedule();
  if (pushed) liveVerify(q.k);
}
rt.quoteFns.push(applyQuote);

function liveVerify(k) {
  live.verify.add(k);
  if (live.verifyTimer) return;
  live.verifyTimer = setTimeout(() => {
    const keys = [...live.verify];
    live.verify.clear();
    live.verifyTimer = 0;
    rtSnapshot(keys);
  }, LIVE_VERIFY_MS);
}

// 整体对一次:拉这批 key 的快照,服务端没给报价的边(而且快照发出之后也没再推来过)
// 就是空了或过期了,从表上拿掉
async function liveResync(keys) {
  const t0 = Date.now();
  const r = await rtSnapshot(keys);
  const own = rt.owners.get("live")?.keys;
  let gone = false;
  for (const k of r.keys) {
    if (r.got.has(k) || r.failed.has(k) || !own?.has(k)) continue;
    const { rk, side } = keySide(k);
    const cur = liveState.get(rk);
    if (!cur?.[side] || cur[side].rx >= t0) continue;
    delete cur[side];
    if (!cur.sell && !cur.buy) liveState.delete(rk);
    gone = true;
  }
  if (gone) liveDirty();
}
setInterval(() => { if (currentView === "live" && !document.hidden) liveResyncNow(); }, LIVE_RESYNC_MS);

// 超过新鲜窗口的边拿掉(服务端不会为它推任何消息)。表上说的是"最近 30 分钟有人看到"。
// 要服务端时间 t 和本机收到的时刻 rx 都过了窗口才拿:只看 t 的话,本机钟比服务端快几分钟时,
// 服务端还认为新鲜的边这里先删掉,下一次整体对账又拿回来,来回闪。rx 由推送和对账刷新,
// 服务端还肯给就一直是新的;真的过期了,实时页开着时由整体对账删,不开着时最多晚一个窗口
function liveExpire() {
  const cut = Date.now() - LIVE_FRESH_MS;
  let gone = false;
  for (const [rk, v] of liveState) {
    for (const side of ["sell", "buy"]) {
      const s = v[side];
      if (s && s.rx < cut && !(s.ts >= cut)) { delete v[side]; gone = true; }
    }
    if (!v.sell && !v.buy) liveState.delete(rk);
  }
  if (gone) liveDirty();
}
setInterval(liveExpire, 15000);
// 切到实时页时对一次账:不在这页时不对账,表上可能留着服务端已经不给的边
function liveResyncNow() {
  const keys = rt.owners.get("live")?.keys;
  if (rt.up && keys?.size) liveResync([...keys]);
}

// /api/coverage 每次都对成交历史全表跑 COUNT(DISTINCT),**不能轮询**:
// 只在启动和 AODP 全量扫描之后读一次。要的是销量榜的城市列表;入库的串城计数新服务端
// 跟着 scan 通知推(noteIngest),这里只是兜老服务端和启动那一刻
async function loadServerCoverage() {
  try {
    const cov = await getJSON("/api/coverage");
    const sel = $("r-city"), keep = sel.value;
    sel.innerHTML = `<option value="__all__">全服</option>` +
      (cov.cities || []).map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join("");
    if (keep && [...sel.options].some(o => o.value === keep)) sel.value = keep;
    noteIngest(cov.ingest);
  } catch (e) { /* 服务端还没起来就先空着 */ }
}

// ── 启动 ───────────────────────────────────────────────────
async function boot() {
  route();
  pullScan("boot");
  rtConnect();
  loadCalibration();
  loadServerCoverage();
  try {
    menuTree = await getJSON("/api/menu");
    $("r-category").innerHTML = `<option value="">全部分类</option>` +
      menuTree.map(c => `<option value="${esc(c.id)}">${esc(c.label)}</option>`).join("");
  } catch (e) { /* 目录还没同步 */ }
}

// 客户端自身状态。这些服务端不知道——它只看得到"有数据传上来",
// 看不到本机抓包到底转没转、Npcap 装没装。
// 在浏览器里直连服务端时 /local/status 是 404,整块就安静地不显示
let localOK = true;
async function pollLocal() {
  if (!localOK) return;
  try {
    const s = await getJSON("/local/status");
    $("capture").innerHTML = s.capturing
      ? '<span class="dot live"></span>抓包中'
      : '<span class="dot dead"></span>未抓包';
    $("capture-sub").textContent = s.orders_uploaded
      ? `已传 ${s.orders_uploaded.toLocaleString("zh-CN")} 条`
      : (s.devices ? `${s.devices} 张网卡` : "本机");
    $("who").textContent = s.character || "—";
    // 地名由客户端本地服务翻好(world.DisplayName),认不出的原样给。以前直接显示原始 id,
    // 成员看到的是 "0007" 而不是 Thetford
    const sub = $("who-sub");
    if (s.location) {
      sub.textContent = s.city || s.location;
      sub.title = s.city && s.city !== s.location ? `地点 id ${s.location}` : `未识别的地点 id ${s.location}`;
    } else if (s.capturing && s.character) {
      // 解析器不知道在哪的时候会直接丢掉挂单(城市单的地点在包里是空的,靠它补)
      sub.textContent = "位置未知,换一次区";
      sub.title = "客户端还没抓到进图的包,这时翻到的挂单不知道是哪座城的,会被丢掉。换一次区(进出一次建筑或传送)就好";
    } else {
      sub.textContent = "角色";
      sub.title = "";
    }

    $("quit").hidden = !s.fallback;

    const warn = $("driver-warn");
    if (s.driver_warning) {
      warn.innerHTML = `<b>抓不到数据:</b>${esc(s.driver_warning)}`;
      warn.hidden = false;
    } else if (s.last_error) {
      warn.innerHTML = `<b>传不上去:</b>${esc(s.last_error)}`;
      warn.hidden = false;
    } else {
      warn.hidden = true;
    }
  } catch (e) {
    // 只有 404 才说明"不是在客户端窗口里跑"。网络抖一下、代理超时
    // 都不该永久关掉这块——Npcap 的警告就挂在这里,关掉了成员
    // 就再也看不到"为什么收不到数据"
    if (e.status === 404) {
      localOK = false;
      for (const id of ["capture", "who"]) $(id).closest(".fact").hidden = true;
    }
  }
}
setInterval(pollLocal, 3000);
pollLocal();

// 退出按钮只在"开不出窗口、退回浏览器"时出现。那种情况下没有窗口
// 可关,发布版又没有控制台,不给个出口就只能去任务管理器杀进程
$("quit").addEventListener("click", async () => {
  if (!confirm("退出程序?抓包会一起停,关掉之后就不再上传数据了。")) return;
  try { await fetch("/local/quit", { method: "POST" }); } catch (e) { /* 它正在退,连接断了是正常的 */ }
  $("quit").textContent = "已退出";
  $("quit").disabled = true;
});

boot();
