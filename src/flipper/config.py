"""配置模型。所有阈值都在这里,方便按实盘反馈调。"""

from __future__ import annotations

from pathlib import Path

import yaml
from pydantic import BaseModel, Field, model_validator

SERVER_BASE_URL = {
    "east": "https://east.albion-online-data.com",
    "west": "https://west.albion-online-data.com",
    "europe": "https://europe.albion-online-data.com",
}


class FreshnessConfig(BaseModel):
    """数据新鲜度。AODP 是众包数据,亚服覆盖率差,这里是第一道也是最重要的闸门。"""

    max_hours: float = 6.0
    """超过这个时间的价格直接丢弃。"""

    high_confidence_hours: float = 2.0
    """双边数据都在这个时间内 → confidence=high。"""


class FilterConfig(BaseModel):
    """Troll 过滤阈值。"""

    deviation_min: float = 0.4
    deviation_max: float = 2.5
    """价格 / 7 日均价 的合理区间,区间外视为 troll 挂单。"""

    min_daily_volume_silver: int = 500_000
    """日白银流水下限。低于这个量的物品吃不下也出不掉。"""

    reject_crossed_book: bool = True
    """买单价 >= 卖单价(交叉盘)。真实市场不可能持续存在这种状态,
    出现说明两侧快照来自不同时间,是陈旧数据的强信号。"""

    max_history_gap_days: int = 3
    """成交历史最后一天距今超过这个天数 → 该物品历史不可用,丢弃。"""

    min_days_with_data_7d: int = 3
    """7 日窗口里至少要有这么多天有成交数据,否则样本太少不可信。"""

    max_margin: float = 1.0
    """单笔毛利率上限。第五层过滤,SPEC 的四层盖不住这个缺口:
    买价偏离 0.45、卖价偏离 2.4 各自都"合规",组合起来却是 5 倍价差 ——
    两边同时是 troll 挂单。真实的同城价差极少超过 100%。"""


class EconomicsConfig(BaseModel):
    """交易经济学。默认值对应亚服 + 高级会员。"""

    market_tax: float = 0.04
    """成交时的市场税。premium 4%,非会员 8%。"""

    setup_fee: float = 0.025
    """挂单手续费,不退。"""

    buy_order_setup_fee: bool = True
    """挂买单是否也收 setup_fee。**已确认收**:游戏本地化文本里买单和卖单都有
    `MARKETPLACE_*ORDER_LABEL_SETUP_COST`,中文管这项叫"创建费"。"""

    outbid_silver: int = 1
    """挂买单要压过现有最高买单才排得到队首,加价幅度。"""

    undercut_silver: int = 1
    """挂卖单要低于现有最低卖单才排得到队首,降价幅度。"""

    @property
    def round_trip_friction(self) -> float:
        """一轮买入 + 卖出的总摩擦比例(不含 outbid/undercut)。"""
        buy_side = self.setup_fee if self.buy_order_setup_fee else 0.0
        return self.market_tax + self.setup_fee + buy_side


class SizingConfig(BaseModel):
    """吃单量估算。倒爷的真正约束是"能吃下市场多大比例而不砸价"。"""

    absorb_ratio: float = 0.20
    """日成交量里我敢吃的比例。第二阶段用实盘成交率反过来校准这个系数。"""

    baseline_days: int = 7
    """troll 过滤的基准均价取最近几天。"""

    history_days: int = 30
    """拉多少天历史。同时给出 7 日和 30 日口径。"""


class ItemSelector(BaseModel):
    """物品清单。用模式展开,不手写 ID;展开结果对 items.txt 校验。"""

    patterns: list[str] = Field(default_factory=list)
    """支持 brace 展开:T{4,5,6}_{CLOTH,METALBAR} → 6 个 ID。"""

    exclude: list[str] = Field(default_factory=list)
    """展开后要剔除的具体 ID。"""


class ApiConfig(BaseModel):
    max_url_length: int = 3500
    """AODP 上限 4096,留余量。超长自动分批。"""

    rate_per_minute: int = 170
    """官方 180/分钟,留余量。"""

    rate_per_5min: int = 280
    """官方 300/5 分钟。注意这条比每分钟那条更紧(等效 56/分钟)。"""

    timeout_seconds: float = 30.0
    max_retries: int = 4


class Config(BaseModel):
    server: str = "east"
    cities: list[str] = Field(default_factory=list)
    capital: int = 10_000_000
    qualities: list[int] = Field(default_factory=lambda: [1])

    freshness: FreshnessConfig = Field(default_factory=FreshnessConfig)
    filters: FilterConfig = Field(default_factory=FilterConfig)
    economics: EconomicsConfig = Field(default_factory=EconomicsConfig)
    sizing: SizingConfig = Field(default_factory=SizingConfig)
    api: ApiConfig = Field(default_factory=ApiConfig)
    items: ItemSelector = Field(default_factory=ItemSelector)

    data_dir: Path = Path("data")

    @model_validator(mode="after")
    def _check(self) -> Config:
        if self.server not in SERVER_BASE_URL:
            raise ValueError(f"未知 server: {self.server},可选 {list(SERVER_BASE_URL)}")
        if not self.cities:
            raise ValueError("cities 不能为空")
        if not self.items.patterns:
            raise ValueError("items.patterns 不能为空")
        return self

    @property
    def base_url(self) -> str:
        return SERVER_BASE_URL[self.server]

    @property
    def db_path(self) -> Path:
        """扫描记录、物品目录、图标共用一个库 —— 备份只要拷这一个文件。"""
        return self.data_dir / "flipper.db"

    @classmethod
    def load(cls, path: Path) -> Config:
        raw = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        return cls.model_validate(raw)
