# 🎬 Video Compressor

[![Release](https://img.shields.io/github/v/release/FuzzyHD1993/video-compressor?sort=semver)](https://github.com/FuzzyHD1993/video-compressor/releases)
[![Release workflow](https://github.com/FuzzyHD1993/video-compressor/actions/workflows/release.yml/badge.svg)](https://github.com/FuzzyHD1993/video-compressor/actions/workflows/release.yml)

Локальный веб-сервис для сжатия видео через FFmpeg. По умолчанию использует
GPU (NVIDIA NVENC), при отсутствии аппаратного энкодера автоматически
переключается на CPU (`libx265`).

## Содержание

- [Возможности](#возможности)
- [Как это работает](#как-это-работает)
- [Быстрый старт (Linux, GPU)](#быстрый-старт-linux-gpu)
- [Запуск без Docker](#запуск-без-docker)
- [Windows: один `.exe`](#-windows-один-exe-без-установки)
- [API](#api)
- [CI и релизы](#ci-и-релизы)
- [Разработка](#разработка)
- [Диагностика](#диагностика)


## Возможности

- 🚀 HEVC/H.264 через NVENC (`hevc_nvenc` / `h264_nvenc`)
- 🖥 Автоматический fallback на CPU (`libx265`)
- 🎚 Три профиля качества: 1080p / 720p / 480p
- 📊 Прогресс обработки в реальном времени
- 🐳 Docker-образ с FFmpeg и GPU passthrough
- 🪟 **Windows: один `.exe`** — FFmpeg и веб-интерфейс встроены, ничего ставить не нужно
- 🧰 Установка на Windows через WSL2 (Docker Desktop или Podman)
- 🔐 Подпись Windows-сборок через SignPath Foundation (при настроенных секретах)
- 📦 Автоматические GitHub Releases по тегам

## Как это работает

1. Браузер отправляет видео на `POST /upload` вместе с профилем качества.
2. Сервер сохраняет файл в `temp/` и ставит задачу в очередь.
3. FFmpeg выбранным энкодером (`hevc_nvenc`, `h264_nvenc`, AMF или `libx265`)
   перекодирует видео с масштабированием и сохранением пропорций.
4. Прогресс передаётся в браузер через `GET /status?jobId=...`.
5. Готовый файл появляется в `output/`; исходник из `temp/` удаляется.

Одновременно выполняется до двух GPU-задач (ограничение NVENC-сессий) или
`runtime.NumCPU()` CPU-задач. Количество можно переопределить через
`MAX_WORKERS`.

### Профили качества

| Профиль | Разрешение | CRF/CQ | Аудио | NVENC preset | CPU preset |
|---|---|---|---|---|---|
| `high` | 1920×1080 | 20 | AAC 192k | `p6` | `slow` |
| `medium` | 1280×720 | 23 | AAC 128k | `p4` | `medium` |
| `low` | 854×480 | 28 | AAC 96k | `p2` | `fast` |

NVENC использует `-rc vbr -cq <CRF> -b:v 0`, CPU — `-crf <CRF>`.

## Быстрый старт (Linux, GPU)

Требования: NVIDIA GPU, драйвер, [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html).

```bash
git clone https://github.com/FuzzyHD1993/video-compressor.git
cd video-compressor
docker compose up -d --build
```

Откройте <http://localhost:8095>.

Проверка, что GPU виден контейнеру:

```bash
docker run --rm --gpus all video-compressor:latest \
  ffmpeg -hide_banner -encoders | grep nvenc
```

### Docker и GPU

`docker-compose.yml` содержит оба способа проброса GPU:

- `deploy.resources` — Docker + NVIDIA Container Toolkit;
- `devices: nvidia.com/gpu=all` — Podman/CDI.

Для Podman CDI-спецификация генерируется один раз:

```bash
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
nvidia-ctk cdi list
```

Полезные команды:

```bash
docker compose logs -f          # логи приложения
docker compose down             # остановить
docker compose up -d --build    # пересобрать и запустить
curl http://localhost:8095/health
```

`temp/` и `output/` смонтированы из репозитория, поэтому результаты видны на
хосте. Внутри контейнера приложение слушает `8080`, наружу опубликован `8095`.

## Запуск без Docker

```bash
sudo apt install ffmpeg        # или pacman -S ffmpeg
go build -o video-compressor ./cmd/server
./video-compressor
```

Сервер слушает `http://localhost:8095`.

### Переменные окружения

| Переменная | По умолчанию | Описание |
|---|---|---|
| `PORT` | `8095` | HTTP-порт |
| `FORCE_CPU` | — | `1` — всегда использовать `libx265` |
| `MAX_WORKERS` | `2` (GPU) / число CPU | Количество параллельных задач |
| `NO_BROWSER` | — | `1` — не открывать браузер |

## Как выбирается энкодер

1. `hevc_nvenc` — HEVC на NVIDIA;
2. `h264_nvenc` — H.264 на NVIDIA;
3. `hevc_amf` / `h264_amf` — AMD (только Windows);
4. `libx265` — CPU fallback.

Пресеты NVENC: `slow → p6`, `medium → p4`, `fast → p2`. Качество задаётся
через `-rc vbr -cq <CRF> -b:v 0`.

## 🪟 Windows: один `.exe` (без установки)

1. Скачайте `video-compressor.exe` из [Releases](https://github.com/FuzzyHD1993/video-compressor/releases).
2. Скопируйте на флешку или локальный диск.
3. Запустите двойным кликом.
4. Откройте <http://localhost:8095>.

**Требования:** Windows 10/11 (64-bit) и NVIDIA GPU (GTX 1660 SUPER или новее)
с драйвером. Docker, WSL2 и FFmpeg не нужны — FFmpeg встроен в `.exe`.

**Особенности:**

- Размер `.exe` — около 105 МБ (включает FFmpeg с NVENC).
- Первый запуск занимает 5–10 секунд: FFmpeg распаковывается в `%TEMP%`
  (кэшируется по хешу, повторные запуски быстрые).
- `temp/` и `output/` создаются рядом с `.exe`; результаты лежат в `output/`.
- Окно консоли должно оставаться открытым — оно и есть сервер.
- При первом запуске Windows может спросить разрешение для брандмауэра (localhost).
- Антивирус/SmartScreen может предупредить о неподписанном Go-бинарнике — это ожидаемо.

### Сборка `.exe` из исходников

```bash
# 1. Скачайте ffmpeg-release-essentials.zip с https://www.gyan.dev/ffmpeg/builds/
#    и положите bin/ffmpeg.exe в cmd/server/ (файл не хранится в git).
cp /path/to/ffmpeg.exe cmd/server/ffmpeg.exe

# 2. Кросс-компиляция под Windows amd64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o video-compressor.exe ./cmd/server

ls -lh video-compressor.exe
```

## Windows (WSL2)

Подробная инструкция: [docs/WINDOWS.md](docs/WINDOWS.md).

Быстрый вариант (PowerShell, Docker Desktop с WSL2 backend):

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-video-compressor.ps1
```

## 🔐 Подпись кода

Windows-сборки подписываются через **SignPath Foundation**, когда в репозитории
настроены секреты `SIGNPATH_API_TOKEN` и `SIGNPATH_ORGANIZATION_ID`. CI собирает
`.exe`, отправляет его в SignPath и прикрепляет подписанный файл к релизу. Если
секретов нет, релиз создаётся с неподписанным `.exe`.

- Политика подписи и инструкция для мейнтейнера: [docs/CODE_SIGNING.md](docs/CODE_SIGNING.md)
- Проверка в Windows: `Get-AuthenticodeSignature .\video-compressor.exe`
- Workflow: [.github/workflows/release.yml](.github/workflows/release.yml)

## API

### `POST /upload`

Принимает `multipart/form-data`:

- `video` — файл видео;
- `quality` — `high`, `medium` или `low` (по умолчанию `medium`).

```bash
curl -F "video=@movie.mkv" -F "quality=medium" \
  http://localhost:8095/upload
# {"jobId":"job_1792340242306434000"}
```

### `GET /status?jobId=...`

```bash
curl "http://localhost:8095/status?jobId=job_1792340242306434000"
```

```json
{
  "id": "job_1792340242306434000",
  "filename": "movie.mkv",
  "status": "done",
  "progress": 100,
  "outputName": "movie_medium.mp4",
  "inputSize": 19578245,
  "outputSize": 5249983
}
```

`status`: `pending` → `processing` → `done` или `error`. При ошибке поле
`error` содержит последние строки лога FFmpeg.

### `GET /health`

```json
{"status":"ok","encoder":"hevc_nvenc","version":"1.1.0"}
```

### `GET /`

Веб-интерфейс (встроен в бинарник).

## CI и релизы

Workflow [`.github/workflows/release.yml`](.github/workflows/release.yml)
запускается по тегам `v*` и вручную:

1. `build-windows` — скачивает FFmpeg с gyan.dev, проверяет SHA-256, запускает
   `gofmt`/`go vet`/`go test` и собирает `video-compressor.exe`;
2. `sign-windows` — отправляет артефакт в SignPath (если заданы секреты) и
   загружает подписанный файл;
3. `release` — прикрепляет подписанный `.exe` (или unsigned-fallback) и
   `SHA256SUMS` к GitHub Release.

Теги с дефисом (`v1.2.0-test`, `v2.0.0-rc1`) публикуются как prerelease.
Подробности и настройка секретов — в [docs/CODE_SIGNING.md](docs/CODE_SIGNING.md).

Локальная сборка релизного `.exe`:

```bash
cp /path/to/ffmpeg.exe cmd/server/ffmpeg.exe   # не хранится в git
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o video-compressor.exe ./cmd/server
```

## Разработка

```bash
gofmt -l .
go vet ./...
go build ./...
go test ./...
```

Запуск локально:

```bash
NO_BROWSER=1 PORT=8095 go run ./cmd/server
```

Кросс-компиляция под Windows:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o video-compressor.exe ./cmd/server
```

### Структура проекта

```text
cmd/server/
  main.go              HTTP-сервер, очередь задач, вызов FFmpeg
  embed_windows.go     встраивание ffmpeg.exe (Windows, build tag)
  embed_other.go       no-op для Linux/Docker
  static.go            встроенный index.html
  static/index.html    веб-интерфейс
  main_test.go         тесты аргументов, энкодера, API
docs/
  WINDOWS.md           WSL2, Docker Desktop, Podman CDI
  CODE_SIGNING.md      политика подписи SignPath
scripts/
  install-video-compressor.ps1
.github/workflows/
  release.yml          сборка, подпись, GitHub Release
Dockerfile
docker-compose.yml
```

### Проверено

| Среда | Результат |
|---|---|
| Linux + GTX 1660 SUPER (FFmpeg 9.x) | ✅ `hevc_nvenc`, 1080p → 720p/480p |
| Docker + NVIDIA Container Toolkit / CDI | ✅ GPU внутри контейнера, upload → HEVC |
| Windows `.exe` под Wine | ✅ распаковка FFmpeg, UI, `/health`, CPU-fallback |
| Реальный Windows + NVIDIA GPU | ⏳ требуется физический хост |

## Диагностика

| Проблема | Решение |
|---|---|
| `FFmpeg не найден` | Установите FFmpeg в `PATH` или положите `ffmpeg.exe` рядом с бинарником; Windows-`.exe` содержит его внутри. |
| Используется `libx265` вместо GPU | Проверьте драйвер NVIDIA, `/dev/nvidia*` в контейнере или `nvidia-smi`; посмотрите `/health`. |
| `nvenc` есть в списке, но задачи падают | Драйвер/библиотеки не проброшены в контейнер; проверьте `NVIDIA_DRIVER_CAPABILITIES=video,compute,utility`. |
| Порт 8095 занят | Задайте другой `PORT` (и обновите проброс порта в compose). |
| Контейнер не видит GPU | `nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml`, перезапустите Docker/Podman. |
| Подпись пропущена в CI | Не заданы `SIGNPATH_API_TOKEN`/`SIGNPATH_ORGANIZATION_ID`; см. [docs/CODE_SIGNING.md](docs/CODE_SIGNING.md). |
| SmartScreen предупреждает | Нажмите «Подробнее → Выполнить всё равно»; репутация накапливается со временем. |
| WSL2: нет `/dev/dxg` | Обновите Windows-драйвер NVIDIA и выполните `wsl --update`; Linux-драйвер внутри WSL не нужен. |

Логи:

```bash
# Linux без Docker
./video-compressor

# Docker
docker compose logs -f

# Windows
# окно консоли, из которого запущен video-compressor.exe
```

## Ограничения

- GTX 1660 SUPER (Turing) поддерживает H.264/HEVC NVENC, но **не AV1**.
- Потребительские GPU имеют лимит одновременных NVENC-сессий, поэтому по
  умолчанию запускается не более двух задач.
- Linux-драйвер NVIDIA внутри WSL2 устанавливать не нужно — используется
  драйвер Windows, проброшенный через `/dev/dxg`.
- Сервис не имеет аутентификации и предназначен для локальной сети. Не
  публикуйте его в интернет без reverse proxy и авторизации.
- Файлы с одинаковым именем и профилем качества перезаписываются в `output/`.
- `.exe` не подписан, пока в репозитории не настроены секреты SignPath.
- FFmpeg распаковывается в `%TEMP%` и не удаляется автоматически; при нехватке
  места удалите `video-compressor-ffmpeg-*.exe` вручную.
