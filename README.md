# APGer - NurOS Package Builder

Автоматизированная система сборки пакетов для NurOS в формате APGv2 с использованием GitHub Actions.

## Возможности

- Автоматическая сборка пакетов из исходного кода
- Генерация метаданных в строгом JSON формате
- Создание контрольных сумм MD5 для всех файлов
- Поддержка пользовательских скриптов (pre/post install/remove)
- Автоматическое создание релизов с `.apg` архивами
- Деплой в корень репозитория для binary sync

## Структура проекта

```
apger/
├── .github/
│   └── workflows/
│       ├── apger-engine.yml    # Переиспользуемый workflow
│       └── build.yml            # Триггер для сборки
├── .ci/
│   ├── recipe.yaml              # Конфигурация пакета
│   └── scripts/                 # Пользовательские скрипты
│       ├── pre-install
│       ├── post-install
│       ├── pre-remove
│       └── post-remove
└── home/                        # Опциональные домашние файлы
```

## Быстрый старт

### 1. Настройка recipe.yaml

Отредактируйте `.ci/recipe.yaml` для вашего пакета:

```yaml
package:
  name: "your-package"
  version: "1.0.0"
  type: "binary"
  architecture: "x86_64"
  description: "Package description"
  maintainer: "Your Name <email@example.com>"
  license: "GPL-3.0"
  homepage: "https://example.com"
  tags: ["tag1", "tag2"]
  dependencies: ["dep1", "dep2 >= 1.0"]
  conflicts: []
  provides: []
  replaces: []
  conf: ["/etc/config.conf"]

source:
  url: "https://example.com/source-1.0.0.tar.xz"

build:
  script: "./configure --prefix=/usr && make"

install:
  script: "make DESTDIR=\"$DESTDIR\" install"
```

### 2. Настройка скриптов (опционально)

Отредактируйте скрипты в `.ci/scripts/`:

- `pre-install` - выполняется перед установкой
- `post-install` - выполняется после установки
- `pre-remove` - выполняется перед удалением
- `post-remove` - выполняется после удалением

### 3. Запуск сборки

Коммит и пуш в ветку `main` автоматически запустит сборку:

```bash
git add .
git commit -m "Update package configuration"
git push origin main
```

Или запустите вручную через GitHub Actions UI.

## Формат APGv2

Созданный `.apg` архив содержит:

```
package-name-version.apg
├── data/              # Установленные файлы (из $DESTDIR)
├── home/              # Домашние файлы (опционально)
├── scripts/           # Пользовательские скрипты
├── metadata.json      # Метаданные пакета
└── md5sums            # Контрольные суммы
```

### Пример metadata.json

```json
{
  "name": "package-name",
  "version": "1.0.0",
  "type": "binary",
  "architecture": "x86_64",
  "description": "Package description",
  "maintainer": "Your Name <email@example.com>",
  "license": "GPL-3.0",
  "tags": ["tag1", "tag2"],
  "homepage": "https://example.com",
  "dependencies": ["dep1", "dep2 >= 1.0"],
  "conflicts": [],
  "provides": [],
  "replaces": [],
  "conf": ["/etc/config.conf"]
}
```

## Binary Deployment

После успешной сборки:

1. Содержимое `apg_root` распаковывается в корень репозитория
2. Создается коммит от `github-actions[bot]`
3. Создается GitHub Release с тегом `v{version}`
4. `.apg` файл прикрепляется к релизу

## Использование как переиспользуемый workflow

В другом репозитории создайте `.github/workflows/build.yml`:

```yaml
name: Build Package

on:
  push:
    branches:
      - main

jobs:
  build:
    uses: your-username/apger/.github/workflows/apger-engine.yml@main
    with:
      recipe_path: '.ci/recipe.yaml'
      scripts_path: '.ci/scripts'
```

## Переменные окружения

Во время сборки доступны:

- `$DESTDIR` - путь для установки файлов (`apg_root/data/`)
- `$PACKAGE_NAME` - имя пакета (в скриптах)
- `$PACKAGE_VERSION` - версия пакета (в скриптах)

## Требования

- Ubuntu runner (GitHub Actions)
- `yq` для парсинга YAML
- `bash` для выполнения скриптов
- Исходный код должен быть доступен по URL

## Типы пакетов

- `binary` - исполняемые программы
- `library` - библиотеки
- `meta` - мета-пакеты (только зависимости)

## Архитектуры

- `x86_64` - AMD64/Intel 64-bit
- `aarch64` - ARM 64-bit
- `any` - независимая от архитектуры

## Лицензия

MIT

## Автор

AnmiTaliDev <anmitali198@gmail.com>
