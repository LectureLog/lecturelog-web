#!/usr/bin/env python3
"""Нормализация OpenAPI 3.1.0 -> вид, понятный oapi-codegen (3.0-совместимый).

oapi-codegen (v2.7.x) пока не умеет nullable в стиле 3.1.0 через
`anyOf: [{...}, {type: "null"}]`. Скрипт схлопывает такие конструкции:

    {"anyOf": [{"type": "string"}, {"type": "null"}]}
        -> {"type": "string", "nullable": true}

Также понижает версию `openapi` с 3.1.0 до 3.0.3, чтобы валидатор внутри
генератора не ругался на 3.1-специфику.

Исходная вендоренная спека НЕ перезаписывается: на вход подаётся openapi.json,
на выход пишется отдельный openapi.normalized.json (промежуточный артефакт).
"""

import json
import sys


def collapse_nullable(node):
    """Рекурсивно обходит схему и схлопывает 3.1.0-nullable в 3.0-nullable."""
    if isinstance(node, list):
        return [collapse_nullable(item) for item in node]
    if not isinstance(node, dict):
        return node

    # Случай anyOf/oneOf с веткой {"type": "null"} — это 3.1.0-nullable.
    for combiner in ("anyOf", "oneOf"):
        variants = node.get(combiner)
        if isinstance(variants, list):
            null_branches = [v for v in variants if isinstance(v, dict) and v.get("type") == "null"]
            non_null = [v for v in variants if not (isinstance(v, dict) and v.get("type") == "null")]
            if null_branches and len(non_null) == 1:
                # Единственный непустой вариант поднимаем на уровень узла и помечаем nullable.
                merged = dict(non_null[0])
                merged["nullable"] = True
                # Переносим сопутствующие ключи исходного узла (description, default, title и т.п.),
                # кроме самого комбинатора.
                for key, value in node.items():
                    if key == combiner:
                        continue
                    merged.setdefault(key, value)
                return collapse_nullable(merged)
            elif null_branches and len(non_null) > 1:
                # Несколько непустых вариантов: оставляем комбинатор без null-ветки, помечаем nullable.
                new_node = {k: v for k, v in node.items() if k != combiner}
                new_node[combiner] = [collapse_nullable(v) for v in non_null]
                new_node["nullable"] = True
                return new_node

    # Обычный рекурсивный обход остальных узлов.
    return {key: collapse_nullable(value) for key, value in node.items()}


def main():
    if len(sys.argv) != 3:
        sys.stderr.write("usage: normalize_openapi.py <input.json> <output.json>\n")
        return 2

    src, dst = sys.argv[1], sys.argv[2]
    with open(src, "r", encoding="utf-8") as f:
        spec = json.load(f)

    # Понижаем версию, чтобы генератор не предупреждал о 3.1.x.
    if str(spec.get("openapi", "")).startswith("3.1"):
        spec["openapi"] = "3.0.3"

    spec = collapse_nullable(spec)

    with open(dst, "w", encoding="utf-8") as f:
        json.dump(spec, f, ensure_ascii=False, indent=2)
        f.write("\n")

    return 0


if __name__ == "__main__":
    sys.exit(main())
