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
const qualityCell = q => `<span class="q" style="--q:var(--q${q || 1})">${QUALITY[q] || q}</span>`;
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
function show(name, push = true) {
  if (!views.includes(name)) name = "desk";
  for (const v of views) $("v-" + v).hidden = v !== name;
  for (const b of document.querySelectorAll("nav button")) {
    if (b.dataset.view === name) b.setAttribute("aria-current", "page");
    else b.removeAttribute("aria-current");
  }
  if (push && location.hash.slice(1).split("/")[0] !== name) location.hash = name;
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
  if (view === "lookup" && arg) loadLookup(decodeURIComponent(arg));
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
let lookupItem = null;

async function loadLookup(itemID) {
  lookupItem = itemID;
  $("lookup-empty").textContent = "查询中…";
  $("lookup-empty").style.display = "";
  $("lookup-rows").innerHTML = "";
  $("results").hidden = true;
  try {
    const res = await getJSON("/api/lookup?item=" + encodeURIComponent(itemID));
    const rows = res.rows || [];
    $("lookup-title").textContent =
      `${res.item.name_zh || res.item.item_id}${res.item.name_en ? "・" + res.item.name_en : ""}・${res.item.item_id}`;
    $("lookup-empty").style.display = rows.length ? "none" : "";
    if (!rows.length) $("lookup-empty").textContent = "这件东西目前没有任何城市有报价。";

    $("lookup-rows").innerHTML = rows.map(r => {
      const spread = r.sell_min && r.buy_max ? r.sell_min - r.buy_max : null;
      return `<tr data-city="${esc(r.city)}" data-quality="${r.quality}">
        <td class="l">${cityDot(r.city)}</td>
        <td class="l">${qualityCell(r.quality)}</td>
        <td>${r.sell_min ? num(r.sell_min) : "—"}</td>
        <td class="depth">${r.source === "capture" ? num(r.sell_qty) : "—"}</td>
        <td data-fill="0" class="sub">${r.source === "capture" ? "…" : "需要抓包"}</td>
        <td>${r.buy_max ? num(r.buy_max) : "—"}</td>
        <td class="depth">${r.source === "capture" ? num(r.buy_qty) : "—"}</td>
        <td data-fill="1" class="sub">${r.source === "capture" ? "…" : "需要抓包"}</td>
        <td class="${spread > 0 ? "pos" : spread < 0 ? "neg" : ""}">${spread === null ? "—" : num(spread)}</td>
        <td class="age">${r.age_hours.toFixed(1)}h</td>
        <td class="l sub">${r.source === "capture" ? "抓包" : "AODP"}</td>
      </tr>`;
    }).join("");
    fillDepths(itemID, rows);
  } catch (e) {
    $("lookup-empty").textContent = String(e.message || e);
  }
}

// 走盘口算实际成交均价。只有抓包覆盖到的格子才有数据,
// 一个格子一个请求,慢慢填进去,不阻塞表格显示
async function fillDepths(itemID, rows) {
  const want = +$("d-qty").value || 1000;
  for (const r of rows) {
    if (r.source !== "capture") continue;
    for (const side of [0, 1]) {
      const q = new URLSearchParams({
        item: itemID, city: r.city, quality: r.quality, side, qty: want,
      });
      try {
        const f = await getJSON("/api/depth?" + q);
        const tr = $("lookup-rows").querySelector(
          `tr[data-city="${CSS.escape(r.city)}"][data-quality="${r.quality}"]`);
        const cell = tr?.querySelector(`[data-fill="${side}"]`);
        if (!cell) continue;
        cell.innerHTML = f.got
          ? `${num(f.vwap)} <span class="slip">滑点 ${pct(f.slippage)}${f.filled ? "" : `・只够 ${num(f.got)} 件`}</span>`
          : "无挂单";
      } catch (e) { /* 某个格子失败不影响其他格子 */ }
    }
  }
}
$("d-qty").addEventListener("change", () => { if (lookupItem) loadLookup(lookupItem); });

let searchTimer;
$("search").addEventListener("input", e => {
  clearTimeout(searchTimer);
  const q = e.target.value.trim();
  if (!q) { $("results").hidden = true; return; }
  searchTimer = setTimeout(async () => {
    showResults(await getJSON("/api/items?limit=40&q=" + encodeURIComponent(q)));
  }, 200);
});

function showResults(items) {
  $("results").hidden = false;
  $("results").innerHTML = items.length
    ? items.map(i => `<div data-id="${esc(i.item_id)}">
        <img src="/api/icon/${encodeURIComponent(i.item_id)}" alt="" loading="lazy"
             onerror="this.style.visibility='hidden'">
        <span>${esc(i.name_zh || i.name_en)}</span>
        <span class="sub">T${i.tier}${i.enchantment ? "." + i.enchantment : ""}・${esc(i.item_id)}</span>
      </div>`).join("")
    : `<div class="sub">没有匹配的物品</div>`;
}
$("results").addEventListener("click", e => {
  const row = e.target.closest("[data-id]");
  if (row) { $("search").value = ""; location.hash = "lookup/" + encodeURIComponent(row.dataset.id); }
});

// ── 三级级联菜单 ───────────────────────────────────────────
let menuTree = null;

$("menu-btn").addEventListener("click", async () => {
  const menu = $("menu");
  if (!menu.classList.contains("bare")) { menu.classList.add("bare"); return; }
  if (!menuTree) menuTree = await getJSON("/api/menu");
  renderMenu([menuTree.map(c => ({ id: c.id, label: c.label, count: c.count, node: c }))]);
  menu.classList.remove("bare");
});
document.addEventListener("click", e => {
  if (!e.target.closest(".picker")) $("menu").classList.add("bare");
});

function renderMenu(columns) {
  $("menu").innerHTML = columns.map((col, depth) =>
    `<ul>` + col.map(x =>
      `<li data-depth="${depth}" data-id="${esc(x.id)}">${esc(x.label)}<span>${x.count}</span></li>`
    ).join("") + `</ul>`).join("");
  $("menu")._columns = columns;
}

// 悬停某一项就把它的下一层追加到右边,和游戏里市场那几个下拉一样
$("menu").addEventListener("mouseover", e => {
  const li = e.target.closest("li");
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

$("menu").addEventListener("click", async e => {
  const li = e.target.closest("li");
  if (!li || +li.dataset.depth !== 2) return;   // 只有最内层的物品族能点
  $("menu").classList.add("bare");
  showResults(await getJSON("/api/items?limit=60&family=" + encodeURIComponent(li.dataset.id)));
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
