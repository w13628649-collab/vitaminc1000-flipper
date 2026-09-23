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
const TREND = { up: "涨", down: "跌", choppy: "震荡", flat: "平" };
const REJECT = {
  one_sided: "单边无数据", stale: "价格过期", stale_history: "历史过期",
  thin_history: "历史样本少", no_history: "没有历史", no_baseline: "没有基准价",
  deviation: "偏离均价(troll)", low_volume: "流水不足", crossed_book: "交叉盘",
  unprofitable: "吃不过摩擦", implausible_margin: "毛利高得不真实",
  too_thin: "可吃量不足 1 件", no_timestamp: "缺时间戳",
};
const MARKET_TAX = 0.04;

const $ = id => document.getElementById(id);
const num = n => Math.round(n).toLocaleString("zh-CN");
const pct = (n, d = 1) => (n * 100).toFixed(d) + "%";
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
  if (name === "lookup") queueFit();   // 隐藏时量不出尺寸,切回来补一次
  if (name === "rank") loadRank();
  if (name === "book") loadBook();
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
function sortable(scope, rows, render) {
  document.querySelector(scope).addEventListener("click", e => {
    const th = e.target.closest("th.s");
    if (!th) return;
    const dir = th.dataset.dir === "desc" ? "asc" : "desc";
    for (const other of th.closest("tr").querySelectorAll("th.s")) delete other.dataset.dir;
    th.dataset.dir = dir;
    render(sortBy(rows(), th.dataset.k, dir));
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

// 两种玩法字段不同,这里抹平成同一个形状——它们竞争的是同一笔钱,
// 分开排名看不出谁更值得投
function unify(res) {
  const out = [];
  for (const o of res.opportunities || []) {
    out.push({
      kind: "flip", item_id: o.item_id, item_name: o.item_name, quality: o.quality,
      from_city: o.city, to_city: o.city,
      mode: o.mode, mode_label: o.mode_label,
      buy_price: o.my_bid, sell_price: o.my_ask,
      cost_per_unit: o.cost_per_unit, profit_per_unit: o.profit_per_unit, margin: o.margin,
      qty: o.qty, capital_used: o.capital_used, daily_profit: o.daily_profit,
      z_score: o.has_z ? o.z_score : null, volatility: o.volatility,
      max_age_hours: o.data_age_hours, confidence: o.confidence,
      warnings: (o.warnings || []).concat(o.hints || []), modes: o.modes || [],
      bottleneck: null, trips: o.turns_per_day, hours: o.hours_per_turn,
    });
  }
  for (const r of res.routes || []) {
    out.push({
      kind: "arb", item_id: r.item_id, item_name: r.item_name, quality: r.quality,
      from_city: r.from_city, to_city: r.to_city,
      mode: r.mode, mode_label: r.mode_label,
      buy_price: r.buy_price, sell_price: r.sell_price,
      cost_per_unit: r.cost_per_unit, profit_per_unit: r.profit_per_unit, margin: r.margin,
      qty: r.qty, capital_used: r.capital_used, daily_profit: r.daily_profit,
      z_score: r.has_z ? r.z_score : null, volatility: r.volatility,
      max_age_hours: r.max_age_hours, confidence: r.confidence,
      warnings: r.warnings || [], modes: r.modes || [],
      bottleneck: r.bottleneck, trips: r.trips_per_day, hours: r.hours_per_trip,
    });
  }
  return out;
}

const BOTTLENECK = { capital: "本金", source: "产地量", dest: "销地量" };

function renderIdeas(rows) {
  const conf = $("i-conf").value;
  const keep = rows.filter(r =>
    (kindFilter === "all" || r.kind === kindFilter) &&
    (!conf || (conf === "high" ? r.confidence === "high" : r.confidence !== "low")));

  $("ideas-empty").style.display = keep.length ? "none" : "";
  $("ideas-meta").textContent = `${keep.length} 条・同城 ${ideas.filter(r => r.kind === "flip").length}・跨城 ${ideas.filter(r => r.kind === "arb").length}`;

  $("ideas-rows").innerHTML = keep.map((r, i) => {
    const route = r.kind === "arb"
      ? `<span class="route">${cityDot(r.from_city)}<span>→</span>${cityDot(r.to_city)}</span>`
      : cityDot(r.from_city);
    const z = r.z_score === null ? "—"
      : `<span class="${r.z_score > 1.5 ? "neg" : r.z_score < -1 ? "pos" : ""}">${r.z_score.toFixed(1)}σ</span>`;
    return `<tr class="open" data-i="${i}">
      <td class="l">${itemCell(r.item_id, r.item_name)}</td>
      <td class="l">${route} <span class="kind ${r.kind}">${r.kind === "arb" ? "跨城" : "同城"}</span></td>
      <td class="l sub">${r.mode_label}</td>
      <td>${num(r.buy_price)}</td>
      <td>${num(r.sell_price)}</td>
      <td class="${r.profit_per_unit > 0 ? "pos" : "neg"}">${num(r.profit_per_unit)}</td>
      <td>${pct(r.margin)}</td>
      <td>${num(r.qty)}${r.bottleneck ? `<span class="sub"> ${BOTTLENECK[r.bottleneck]}</span>` : ""}</td>
      <td>${num(r.capital_used)}</td>
      <td class="sub">${r.trips ? r.trips.toFixed(1) : "—"}</td>
      <td><b>${num(r.daily_profit)}</b></td>
      <td>${z}</td>
      <td>${r.volatility ? r.volatility.toFixed(2) : "—"}</td>
      <td class="age">${r.max_age_hours.toFixed(1)}h</td>
      <td class="l"><span class="tag ${r.confidence}">${CONFIDENCE[r.confidence]}</span></td>
    </tr>`;
  }).join("");
  $("ideas-rows")._rows = keep;
}

// 点开一行显示四种执行方式。最赚的那个往往两头都要等,
// 用户可能宁愿要快的——这个选择不该由代码替他做
$("ideas-rows").addEventListener("click", e => {
  const tr = e.target.closest("tr.open");
  if (!tr || e.target.closest("a") || e.target.closest("button")) return;
  const next = tr.nextElementSibling;
  if (next && next.classList.contains("detail")) { next.remove(); return; }
  for (const d of $("ideas-rows").querySelectorAll("tr.detail")) d.remove();

  const r = $("ideas-rows")._rows[+tr.dataset.i];
  const modes = r.modes.length ? r.modes : [];
  const warn = r.warnings.length
    ? `<p class="note">${r.warnings.map(esc).join("・")}</p>` : "";
  tr.insertAdjacentHTML("afterend", `<tr class="detail"><td colspan="15">
    <table class="tight"><thead><tr>
      <th class="l">执行方式</th><th>买入价</th><th>卖出价</th>
      <th>单件利润</th><th>毛利率</th><th>摩擦</th>
      <th>一轮耗时</th><th>轮/天</th><th>日收益</th><th class="l">说明</th>
    </tr></thead><tbody>
    ${modes.map(m => {
      const hours = m.hours_per_trip ?? m.hours_per_turn;
      const turns = m.trips_per_day ?? m.turns_per_day;
      return `<tr>
      <td class="l"><b>${esc(m.label)}</b>${m.mode === r.mode ? ' <span class="tag high">选用</span>' : ""}</td>
      <td>${num(m.buy_price)}</td><td>${num(m.sell_price)}</td>
      <td class="${m.profit_per_unit > 0 ? "pos" : "neg"}">${num(m.profit_per_unit)}</td>
      <td>${pct(m.margin)}</td><td>${pct(m.friction, 1)}</td>
      <td class="sub">${hours ? hours.toFixed(1) + "h" : "—"}</td>
      <td class="sub">${turns ? turns.toFixed(1) : "—"}</td>
      <td class="${m.daily_profit > 0 ? "pos" : "neg"}"><b>${m.daily_profit != null ? num(m.daily_profit) : "—"}</b></td>
      <td class="l sub">${MODE_NOTE[m.mode] || ""}</td>
    </tr>`}).join("")}
    </tbody></table>
    ${warn}
    <p class="note">
      <b>选用的是日收益最高的那个,不是单件利润最高的。</b>挂单腿要等成交,
      一天转不了几轮;秒买秒卖单件少,周转快起来总量可能反超。<br>
      ${r.trips ? `一轮 ${(r.hours || 0).toFixed(1)} 小时,一天 ${r.trips.toFixed(1)} 轮・` : ""}
      占用本金 ${num(r.capital_used)}・日收益 ${num(r.daily_profit)}
      <button class="mini" data-log="${+tr.dataset.i}">记一笔</button>
    </p>
  </td></tr>`);
});

const MODE_NOTE = {
  "taker-taker": "立刻成交,不用等。付卖一、收买一,只交 4% 税",
  "taker-maker": "货立刻到手,卖单挂着等。摩擦 6.5%",
  "maker-taker": "买单挂着等,拿到货立刻脱手。摩擦 6.5%",
  "maker-maker": "价格最好,但两头都要等成交。摩擦 9%",
};

$("kind-seg").addEventListener("click", e => {
  const b = e.target.closest("button[data-kind]");
  if (!b) return;
  kindFilter = b.dataset.kind;
  for (const other of $("kind-seg").children)
    other.setAttribute("aria-pressed", other === b ? "true" : "false");
  renderIdeas(ideas);
});
$("i-conf").addEventListener("change", () => renderIdeas(ideas));
sortable("#v-ideas thead", () => ideas, renderIdeas);

function applyScan(res) {
  scan = res;
  ideas = sortBy(unify(res), "daily_profit", "desc");
  renderIdeas(ideas);
  renderCoverage(res.coverage || []);
  const counts = Object.entries(res.reject_counts || {}).sort((a, b) => b[1] - a[1]);
  $("rejects").textContent = counts.length
    ? "被过滤掉的:" + counts.map(([k, v]) => `${REJECT[k] || k} ${v}`).join("、") : "";
  const age = (Date.now() - new Date(res.started_at)) / 3600000;
  $("scan-age").textContent = age < 1 ? `${Math.round(age * 60)} 分钟前` : `${age.toFixed(1)} 小时前`;
  $("scan-meta").textContent =
    `${res.item_ids.length} 个物品・${res.price_rows} 条报价・${res.request_count} 次请求`;
  loadPortfolio();
}

function renderCoverage(coverage) {
  $("cov").innerHTML = coverage.map(c => {
    const ratio = c.with_data ? c.within_threshold / c.with_data : 0;
    return `<div>
      <b>${esc(c.city)}</b>
      <div class="track"><div class="fill" style="width:${(ratio * 100).toFixed(0)}%;--c:var(${CITY_VAR[c.city] || "--ink-soft"})"></div></div>
      <i>${c.within_threshold}/${c.with_data} 新鲜${c.has_median ? "・中位 " + c.median_age_hours.toFixed(1) + "h" : ""}</i>
    </div>`;
  }).join("");
}

async function loadScan(force) {
  try {
    applyScan(await getJSON("/api/scan", force ? { method: "POST" } : undefined));
  } catch (e) {
    $("ideas-empty").textContent = String(e.message || e);
    $("ideas-empty").style.display = "";
  }
}
$("rescan").addEventListener("click", async () => {
  const b = $("rescan");
  b.disabled = true; b.textContent = "扫描中…";
  await loadScan(true);
  b.disabled = false; b.textContent = "重新扫描";
});

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
  const idlePct = p.capital ? p.idle / p.capital : 0;
  $("kpis").innerHTML = `
    <div><b>${num(p.daily_profit)}</b><i>日收益(银)</i>
      <em>${p.slices.length} 个仓位</em></div>
    <div><b>${pct(p.daily_roi, 2)}</b><i>本金日回报</i>
      <em>年化没有意义,市场吃不下</em></div>
    <div><b>${num(p.deployed)}</b><i>已部署</i>
      <em>闲置 ${num(p.idle)}(${pct(idlePct, 0)})</em></div>
    <div><b>${num(p.slices.reduce((a, s) => a + s.qty, 0))}</b><i>总件数</i>
      <em>跨城 ${p.slices.filter(s => s.kind === "arb").length} 条</em></div>`;
  $("p-note").textContent = p.note || "";

  $("p-rows").innerHTML = p.slices.map(s => `<tr>
    <td class="l">${esc(s.label)}</td>
    <td class="l"><span class="kind ${s.kind}">${s.kind === "arb" ? "跨城" : "同城"}</span></td>
    <td>${num(s.qty)}</td>
    <td>${num(s.capital)}</td>
    <td><b>${num(s.daily_profit)}</b></td>
    <td>${pct(s.roi)}</td>
    <td class="sub" title="风险调整后,排序按这个">${s.risk_adj_roi ? pct(s.risk_adj_roi) : "—"}</td>
    <td class="sub">${s.volatility ? s.volatility.toFixed(2) : "—"}</td>
    <td class="sub">${s.turns_per_day ? s.turns_per_day.toFixed(1) : "—"}</td>
  </tr>`).join("");

  renderCurve(p);
}

// 累计投入 vs 累计收益。曲线越往右越平,就是边际收益递减——
// 一眼能看出"再投下去不值得"的拐点在哪
function renderCurve(p) {
  const pts = p.slices;
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
        <em>实际净利 / 计划净利</em></div>
    </div>`;
  } catch (e) { $("calib").innerHTML = `<p class="note">${esc(e.message || e)}</p>`; }
}

// ── 记账 ───────────────────────────────────────────────────
let pendingTrade = null;

$("ideas-rows").addEventListener("click", e => {
  const b = e.target.closest("button[data-log]");
  if (!b) return;
  e.stopPropagation();
  const r = $("ideas-rows")._rows[+b.dataset.log];
  pendingTrade = r;
  $("td-title").textContent = "记一笔:" + r.item_name;
  $("td-sub").textContent =
    `${r.mode_label}・${r.from_city}${r.kind === "arb" ? " → " + r.to_city : ""}・` +
    `买 ${num(r.buy_price)} 卖 ${num(r.sell_price)}`;
  const form = $("trade-form");
  form.owner.value = localStorage.getItem("owner") || "";
  form.qty.value = r.qty;
  $("trade-dialog").showModal();
});

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
        market_daily_qty: r.kind === "arb" ? 0 : (scan?.opportunities || [])
          .find(o => o.item_id === r.item_id && o.city === r.from_city)?.daily_volume_qty || 0,
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
    $("book-empty").style.display = rows.length ? "none" : "";
    $("book-meta").textContent = `${rows.length} 笔`;
    $("book-rows").innerHTML = rows.map(t => `<tr>
      <td class="l">${itemCell(t.item_id, t.item_id)}</td>
      <td class="l">${t.kind === "arb"
        ? `<span class="route">${cityDot(t.buy_city)}<span>→</span>${cityDot(t.sell_city)}</span>`
        : cityDot(t.buy_city)}</td>
      <td class="l sub">${esc(t.mode)}</td>
      <td>${num(t.planned_qty)}</td>
      <td>${num(t.planned_buy)}</td>
      <td>${num(t.planned_sell)}</td>
      <td>${t.filled_buy_qty != null ? num(t.filled_buy_qty) : "—"}</td>
      <td class="${t.realized_profit > 0 ? "pos" : t.realized_profit < 0 ? "neg" : ""}">
        ${t.realized_profit != null ? num(t.realized_profit) : "—"}</td>
      <td class="l"><span class="tag ${t.status === "filled" ? "high" : t.status === "open" ? "medium" : "low"}">${STATUS[t.status] || t.status}</span></td>
      <td class="l">${t.status === "open"
        ? `<button class="mini" data-close="${t.id}" data-qty="${t.planned_qty}">收口</button> ` : ""
        }<button class="mini" data-del="${t.id}">删</button></td>
    </tr>`).join("");
    loadCalibration();
  } catch (e) {
    $("book-empty").textContent = String(e.message || e);
    $("book-empty").style.display = "";
  }
}
const STATUS = { open: "未收口", filled: "已成交", partial: "部分成交", abandoned: "放弃" };

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
async function selectItem(itemId) {
  if (!itemId) return;
  const seq = ++lookup.seq;
  // 同一件重查(页面开久了、从别的页跳回来)时旧表先留着,新数据回来再换,别闪成"正在查"
  const again = itemId === lookup.itemId && !!lookup.data;
  lookup.itemId = itemId;
  lookup.pending = true;
  if (!again) lookup.data = null;
  markListSelection();
  const h = "#lookup/" + encodeURIComponent(itemId);
  if (location.hash !== h) location.hash = h;   // route() 看到请求在飞会跳过
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
    renderPrices(d);
    // 同一件重查过了,右栏的阶梯也得跟着换成这一刻的,不然面板上下两半是两个时间的数
    if (ladder.key && ladder.itemId === itemId) loadLadder(++ladder.seq, { grid: false });
    // 从别的页面点物品名跳过来、这件又不在左栏里:照它的分类重填左栏。
    // 每次都要判断,不是只有第一次 —— 旧版 gotoItem 就是每次都重填。
    // 用户自己在搜索框里搜着的时候不动
    const inList = lookup.list.some(it => it.item_id === itemId);
    if (!inList && !$("search").value.trim() && d.item?.category) {
      lookup.sel = { cat: d.item.category, sub: d.item.subcategory || "", fam: d.item.family || "" };
      refreshList({ reveal: true });
    }
  } catch (err) {
    if (seq !== lookup.seq) return;
    lookup.pending = false;
    lookup.data = null;
    box.innerHTML = `<div class="empty"><h2>查不到</h2><p>${esc(err.message || err)}</p></div>`;
    closeLadder();
  }
}

function renderPrices(d) {
  const P = d.params || {};
  const maxH = P.max_hours ?? 6;
  const minBid = P.min_bid_depth ?? 20;
  const qualities = d.qualities?.length ? d.qualities : [1];
  const fresh = s => s && s.best > 0 && s.age_hours != null && s.age_hours <= maxH;

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

  // 一侧的来源说明:两路都列出来,说清楚用的是哪一路、为什么
  const srcHint = s => {
    const lines = [];
    if (s.capture) lines.push(`抓包 ${num(s.capture.best)}(${ageText(s.capture.age_hours)} 前)· 最优档 ${num(s.capture.qty_at_best)} 件 / ${Number(s.capture.orders_at_best)} 单 · 5% 以内 ${num(s.capture.qty_near)} 件`);
    if (s.capture?.prev_page_orders) lines.push(`最近一次只翻到了后面的页,前面 ${Number(s.capture.prev_page_orders)} 张单是更早那一页看到的,照样算在内`);
    if (s.aodp) lines.push(`AODP ${num(s.aodp.best)}(${s.aodp.age_hours == null ? "没有时间戳" : ageText(s.aodp.age_hours) + " 前"})`);
    if (s.capture && s.aodp) lines.push(`用的是${s.pick === "capture" ? "抓包" : "AODP"}:两路谁新用谁,一样新用抓包`);
    return lines.join("\n");
  };
  // 买卖两侧的时间戳常常差很远,各报各的龄。标签用游戏里市场那两个页签的说法
  const side = (label, s, hint) => {
    const stale = s.best && (s.age_hours == null || s.age_hours > maxH);
    const src = srcHint(s);
    return `<div class="ln" title="${esc(hint + (src ? "\n\n" + src : ""))}"><u>${label}</u><b>${s.best ? num(s.best) : "—"}</b>
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
      parts.push(`<em class="qty${dim ? " dim" : ""}" title="${esc(`最优档 ${num(cap.qty_at_best)} 件,最优价 5% 以内共 ${num(cap.qty_near)} 件` +
        (dim ? "\n抓包比 AODP 旧,价格用的是 AODP,件数只作参考" : ""))}">×${num(cap.qty_at_best)}${cap.qty_near > cap.qty_at_best ? "/" + num(cap.qty_near) : ""}</em>`);
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
      ${side("卖单最低", c.sell, "市场上最便宜的那张卖单。你想马上买到货,就付这个价")}${extra(c.sell, "sell")}
      ${side("买单最高", c.buy, "市场上出价最高的那张买单。你想马上出货,就拿这个价")}${extra(c.buy, "buy")}
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
      你想马上出货拿的就是它。<b>×N/M</b> 是最优档件数 / 最优价 5% 以内的件数,只有自建抓包拿得到;
      带 <i class="src" style="margin:0">抓</i> 的那一侧用的是抓包,没带的是 AODP。两路谁新用谁。</p>
      <p>所以<b>卖单价总是比买单价高</b>,这段差就是倒爷的利润空间。<b>同城价差</b>已经替你把
      ${pct(P.friction ?? 0.09)} 的税和手续费扣掉了(真实盈亏平衡价差 ${pct(P.breakeven ?? 0.0963, 2)}):
      在这座城挂买单收货、再挂卖单出货,一轮下来的净毛利率。为正才值得做。
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
  // 两条滚动条互相跟。赋同一个值不会再触发 scroll,不会来回弹
  bar.addEventListener("scroll", () => { wrap.scrollLeft = bar.scrollLeft; }, { passive: true });
  wrap.addEventListener("scroll", () => { bar.scrollLeft = wrap.scrollLeft; }, { passive: true });
  fitLookup();
  if (keep) {
    wrap.scrollLeft = keep.x;
    if (keep.focus) wrap.querySelector(".cellbtn.open")?.focus({ preventScroll: true });
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
    // 告诉人右边还藏着哪几列,不然五档品质只看得到两档半,也不知道要滚
    const cut = wrap.getBoundingClientRect().right;
    const hiddenQ = [...wrap.querySelectorAll("thead th.q")]
      .filter(th => th.getBoundingClientRect().right > cut + 1).map(th => th.textContent.trim());
    note.textContent = `表比这一栏宽${hiddenQ.length ? `,右边还有 ${hiddenQ.join("、")}` : ""}:拖上面这条横向滚动,或者用「品质」只看一档`;
  }
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
  const stats = [
    stat("最低卖价", cs.best ? num(cs.best) : "—", `别人挂着卖的最低价。${srcOf(cs)}`),
    stat("最高买价", cb.best ? num(cb.best) : "—", `别人挂着收的最高价。${srcOf(cb)}`),
    stat("买卖价差", sp ? pct(sp.raw) : "—", "未扣税费的原始价差"),
    stat("税后毛利", sp ? pct(sp.margin) : "—",
      `挂买收货再挂卖出货,扣掉 ${pct(P.friction ?? 0.09)} 摩擦后的净毛利率;真实盈亏平衡价差 ${pct(P.breakeven ?? 0.0963, 2)}`),
    stat("挂买→挂卖", sp ? `${num(sp.my_bid)}→${num(sp.my_ask)}` : "—", "进游戏照抄的两个数(各让 1 银抢队首)"),
    stat("单件净赚", sp ? num(sp.profit_per_unit) : "—", "税后"),
    stat("7日 / 30日均价", h ? `${h.avg_7d ? num(h.avg_7d) : "—"} / ${num(h.avg_30d)}` : "—",
      "成交量加权,不含今天。零星成交日和万笔成交日不等权"),
    stat("成交区间", h?.price_min ? `${num(h.price_min)}~${num(h.price_max)}` : "—", "30 日窗口里各天均价的最低 ~ 最高"),
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
      ${thinBid ? `<br>而且最高买价 5% 以内只有 ${num(buy.support.qty_near)} 件在收。` : ""}
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
  $("rank-rows").innerHTML = rows.map(r => `<tr>
    <td class="l">${itemCell(r.item_id, r.item_name)}</td>
    <td class="l">${r.city === "全服" ? "全服" : cityDot(r.city)}</td>
    <td>${num(r.daily_qty)}</td>
    <td><b>${num(r.daily_silver)}</b></td>
    <td>${num(r.avg_price)}</td>
    <td>${num(r.price_min)}</td>
    <td>${num(r.price_median)}</td>
    <td>${num(r.price_max)}</td>
    <td>${r.volatility.toFixed(2)}</td>
    <td class="${r.trend === "up" ? "pos" : r.trend === "down" ? "neg" : ""}"
        title="R² ${r.trend_fit.toFixed(2)}">${TREND[r.trend]} ${r.trend_pct >= 0 ? "+" : ""}${r.trend_pct.toFixed(1)}%</td>
    <td class="l ${TREND_CLASS[r.trend] || ""}">${sparkline(r.series)}</td>
    <td class="sub">${r.days_with_data} 天</td>
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
    renderRank(rankRows);
    $("rank-meta").textContent = `${rankRows.length} 条`;
  } catch (e) {
    $("rank-empty").textContent = String(e.message || e);
    $("rank-empty").style.display = "";
  }
}
for (const id of ["r-city", "r-window", "r-quality", "r-category"])
  $(id).addEventListener("change", loadRank);
sortable("#v-rank thead", () => rankRows, renderRank);

// ── 实时行情 ───────────────────────────────────────────────
const liveState = new Map();
const WATCH_ITEMS = ["T4_METALBAR", "T5_METALBAR", "T6_METALBAR", "T5_CLOTH", "T5_PLANKS"];
const WATCH_CITIES = ["Martlock", "Lymhurst", "Bridgewatch", "Thetford"];
const keys = [];
for (const it of WATCH_ITEMS)
  for (const c of WATCH_CITIES)
    for (const side of [0, 1]) keys.push(`${it}|${c}|1|${side}`);

function renderLive() {
  const rows = [...liveState.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  $("live-empty").style.display = rows.length ? "none" : "";
  $("live-rows").innerHTML = rows.map(([rk, v]) => {
    const [item, city] = rk.split("|");
    return `<tr data-rk="${rk}">
      <td class="l">${esc(item)}</td><td class="l">${cityDot(city)}</td>
      <td><span class="price" data-f="sell">${v.sell ? num(v.sell.p) : "—"}</span></td>
      <td class="depth">${v.sell ? num(v.sell.d) : ""}</td>
      <td><span class="price" data-f="buy">${v.buy ? num(v.buy.p) : "—"}</span></td>
      <td class="depth">${v.buy ? num(v.buy.d) : ""}</td>
      <td class="age">${v.at ? new Date(v.at).toLocaleTimeString("zh-CN") : ""}</td>
    </tr>`;
  }).join("");
}

// 只改变化的那个单元格并让它闪一下,不整表重绘
function patch(rk, field, q, dir) {
  const tr = document.querySelector(`tr[data-rk="${CSS.escape(rk)}"]`);
  if (!tr) { renderLive(); return; }
  const el = tr.querySelector(`[data-f="${field}"]`);
  if (!el) return;
  el.textContent = num(q.p);
  // 深度在下一个 td 里,不是 span 的兄弟——span 是这个 td 里唯一的元素
  const depthCell = el.closest("td").nextElementSibling;
  if (depthCell) depthCell.textContent = num(q.d);
  tr.lastElementChild.textContent = new Date(q.t).toLocaleTimeString("zh-CN");
  if (dir) {
    el.classList.remove("flash-up", "flash-down");
    void el.offsetWidth;                 // 强制重排,让同一元素能连续播动画
    el.classList.add("flash-" + dir);
  }
}

function applyQuote(q) {
  const p = q.k.split("|");
  const rk = `${p[0]}|${p[1]}|${p[2]}`;
  const side = p[3] === "0" ? "sell" : "buy";
  const cur = liveState.get(rk) || {};
  const prev = cur[side];
  const dir = prev ? (q.p > prev.p ? "up" : q.p < prev.p ? "down" : "") : "";
  cur[side] = { p: q.p, d: q.d };
  cur.at = q.t;
  liveState.set(rk, cur);
  if (!prev) renderLive(); else patch(rk, side, { p: q.p, d: q.d, t: q.t }, dir);
}

let backoff = 500;
function connect() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${proto}//${location.host}/ws`);
  ws.onopen = async () => {
    backoff = 500;
    $("conn").innerHTML = '<span class="dot live"></span>已连接';
    // ① 重新订阅:新连接(可能是另一个实例)完全不知道你要看什么
    ws.send(JSON.stringify({ op: "sub", keys }));
    // ② 拉快照:补上断线期间错过的变化。少了这步界面会停在旧值上,
    //    却看起来"一切正常"——这是这类系统最常见的 bug
    try { (await getJSON("/api/quotes?keys=" + keys.join(","))).forEach(applyQuote); } catch (e) { }
  };
  ws.onmessage = e => applyQuote(JSON.parse(e.data));
  ws.onclose = () => {
    $("conn").innerHTML = '<span class="dot dead"></span>重连中…';
    setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, 30000);   // 指数退避
  };
}

// ── 启动 ───────────────────────────────────────────────────
async function boot() {
  route();
  connect();
  loadScan(false);
  loadCalibration();
  try {
    const cov = await getJSON("/api/coverage");
    $("r-city").innerHTML = `<option value="__all__">全服</option>` +
      (cov.cities || []).map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join("");
  } catch (e) { /* 服务端还没起来就先空着 */ }
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
    $("who-sub").textContent = s.location ? s.location : "角色";

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
