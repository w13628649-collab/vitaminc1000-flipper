from __future__ import annotations

from pathlib import Path

from flipper.catalog import (
    Item,
    ItemCatalog,
    expand_braces,
    family_label,
    family_of,
    longest_common_suffix,
)

CATEGORIES = [
    {"id": "weapons", "parent": "", "label_zh": "武器", "label_en": "Weapons", "sort": 0},
    {"id": "crafting", "parent": "", "label_zh": "制作", "label_en": "Crafting", "sort": 1},
    {"id": "firestaff", "parent": "weapons", "label_zh": "火焰法杖",
     "label_en": "Fire Staff", "sort": 0},
    {"id": "refinedresources", "parent": "crafting", "label_zh": "加工资源",
     "label_en": "Refined Resources", "sort": 0},
]

CLOTH4 = Item("T4_CLOTH", "细布", "Fine Cloth", "crafting", "refinedresources", "CLOTH", 4, 0, 1)
CLOTH5 = Item("T5_CLOTH", "精布", "Ornate Cloth", "crafting", "refinedresources", "CLOTH", 5, 0, 1)
STAFF = Item("T4_2H_FIRESTAFF@1", "老手级杰出火焰法杖", "Adept's Great Fire Staff",
             "weapons", "firestaff", "2H_FIRESTAFF", 4, 1, 5)


def build(*items: Item) -> ItemCatalog:
    return ItemCatalog(Path("/nonexistent.db"), {i.item_id: i for i in items}, CATEGORIES)


def test_brace展开():
    assert expand_braces("T{4,5}_{ORE,FIBER}") == ["T4_ORE", "T4_FIBER", "T5_ORE", "T5_FIBER"]
    assert expand_braces("T4_CLOTH") == ["T4_CLOTH"]
    assert expand_braces("T{4, 5}_ORE") == ["T4_ORE", "T5_ORE"]  # 容忍空格


def test_展开结果对目录校验并报出不存在的id():
    resolved, missing = build(CLOTH4, CLOTH5, STAFF).resolve(["T{4,5,6}_CLOTH"])
    assert resolved == ["T4_CLOTH", "T5_CLOTH"]
    assert missing == ["T6_CLOTH"]  # 手写清单会在游戏更新后悄悄失效,必须报出来


def test_去重与排除():
    resolved, _ = build(CLOTH4, CLOTH5).resolve(["T4_CLOTH", "T{4,5}_CLOTH"],
                                                exclude=["T5_CLOTH"])
    assert resolved == ["T4_CLOTH"]


def test_显示名优先用官方中文():
    cat = build(CLOTH4, CLOTH5)
    assert cat.name_of("T5_CLOTH") == "精布"
    assert cat.en_name_of("T5_CLOTH") == "Ornate Cloth"


def test_附魔物品显示名补回等级():
    # dump 里附魔物品和本体同名,列表里会分不清
    assert build(STAFF).name_of("T4_2H_FIRESTAFF@1") == "老手级杰出火焰法杖.1"


def test_未知id回落成id本身():
    assert build(CLOTH4).name_of("T9_UNKNOWN") == "T9_UNKNOWN"


def test_搜索中英文和id都能命中():
    cat = build(CLOTH4, CLOTH5, STAFF)
    assert [i.item_id for i in cat.search("细布")] == ["T4_CLOTH"]
    assert [i.item_id for i in cat.search("ornate")] == ["T5_CLOTH"]
    assert [i.item_id for i in cat.search("T4_CLOTH")] == ["T4_CLOTH"]
    assert [i.item_id for i in cat.search("布")] == ["T4_CLOTH", "T5_CLOTH"]
    assert cat.search("  ") == []


def test_搜索结果精确匹配排在前面():
    # "细布" 是 T4 的全名,T5 的"精布"只是子串命中
    assert build(CLOTH4, CLOTH5).search("布")[0].item_id == "T4_CLOTH"


def test_按分类和等级浏览():
    cat = build(CLOTH4, CLOTH5, STAFF)
    assert [i.item_id for i in cat.browse(category="crafting")] == ["T4_CLOTH", "T5_CLOTH"]
    assert [i.item_id for i in cat.browse(subcategory="firestaff")] == ["T4_2H_FIRESTAFF@1"]
    assert [i.item_id for i in cat.browse(category="crafting", tier=5)] == ["T5_CLOTH"]


def test_按物品族浏览():
    cat = build(CLOTH4, CLOTH5, STAFF)
    assert [i.item_id for i in cat.browse(family="CLOTH")] == ["T4_CLOTH", "T5_CLOTH"]


def test_按附魔浏览():
    cat = build(CLOTH4, CLOTH5, STAFF)
    # enchantment=0 是"只要无附魔的",不是"不筛"——后者用 -1
    assert [i.item_id for i in cat.browse(enchantment=0)] == ["T4_CLOTH", "T5_CLOTH"]
    assert [i.item_id for i in cat.browse(enchantment=1)] == ["T4_2H_FIRESTAFF@1"]
    assert len(cat.browse(enchantment=-1)) == 3


def test_族key剥掉等级前缀附魔后缀和材料的LEVEL中缀():
    assert family_of("T5_2H_FIRESTAFF@2") == "2H_FIRESTAFF"
    assert family_of("T4_CLOTH") == "CLOTH"
    # 材料的附魔是独立条目,得跟本体归到一族
    assert family_of("T4_CLOTH_LEVEL1@1") == "CLOTH"


def test_族名取公共后缀但只要多数成员满足():
    """火焰法杖那族里混着一件叫「Vendetta之怒」的特殊武器。

    按全员取交集会把后缀打成空字符串,整族就没名字了。
    """
    names = ["老手级杰出火焰法杖", "专家级杰出火焰法杖", "大师级杰出火焰法杖", "Vendetta之怒"]
    assert longest_common_suffix(names) == "杰出火焰法杖"
    assert longest_common_suffix(["专家级弓箭", "大师级弓箭", "新手级弓箭"]) == "弓箭"
    assert longest_common_suffix(["细布"]) == "细布"


def test_材料族名退回最低等级的全名():
    """细布/精布/华布 的公共后缀只剩一个"布"字,金属锭更是只剩"条"。"""
    cloths = [CLOTH4, CLOTH5]
    assert family_label(cloths) == "细布"  # 不是"布"
    staffs = [
        Item("T4_2H_FIRESTAFF", "老手级杰出火焰法杖", "", "", "", "2H_FIRESTAFF", 4, 0, 5),
        Item("T5_2H_FIRESTAFF", "专家级杰出火焰法杖", "", "", "", "2H_FIRESTAFF", 5, 0, 5),
    ]
    assert family_label(staffs) == "杰出火焰法杖"


def test_附魔物品继承本体的分类():
    """附魔变体不在原始 dump 里,分类得靠剥掉 @N 后的本体查。"""
    staff = build(STAFF).get("T4_2H_FIRESTAFF@1")
    assert staff.base_id == "T4_2H_FIRESTAFF"
    assert (staff.category, staff.subcategory) == ("weapons", "firestaff")
    assert staff.enchantment == 1


def test_三级菜单树只含有物品的分类():
    tree = build(CLOTH4, CLOTH5, STAFF).menu_tree()
    assert [n["id"] for n in tree] == ["weapons", "crafting"]  # 顺序照搬游戏内市场

    crafting = next(n for n in tree if n["id"] == "crafting")
    assert crafting["label"] == "制作"
    assert crafting["count"] == 2
    sub = crafting["subs"][0]
    assert (sub["id"], sub["label"]) == ("refinedresources", "加工资源")
    assert [f["key"] for f in sub["families"]] == ["CLOTH"]
    assert sub["families"][0]["tiers"] == [4, 5]


def test_材料和装备的品质档数():
    cat = build(CLOTH4, STAFF)
    # 材料只有普通一档,界面据此禁用品质筛选
    assert cat.get("T4_CLOTH").max_quality == 1
    assert cat.get("T4_2H_FIRESTAFF@1").max_quality == 5
