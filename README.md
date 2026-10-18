# 🎬 Video Compressor v2

[![Release](https://img.shields.io/github/v/release/FuzzyHD1993/video-compressor?sort=semver)](https://github.com/FuzzyHD1993/video-compressor/releases)
[![Release workflow](https://github.com/FuzzyHD1993/video-compressor/actions/workflows/release.yml/badge.svg)](https://github.com/FuzzyHD1993/video-compressor/actions/workflows/release.yml)

Локальный веб-сервис для сжатия видео через FFmpeg. Программа сама подбирает
лучшее ускорение и берёт файлы по одному.

## Возможности

- 🚀 **Универсальный энкодер:** NVENC → QSV → AMF → libx265/libx264
- 📹 **Несколько видео подряд** с прогрессом и отменой
- 📁 **Сжать папку** — программа найдёт все видео внутри
- 🎚 **Три режима качества:** Быстро / Баланс / Качество
- 🖥 Автоматический CPU-fallback с ограничением потоков
- 🔐 Basic Auth для доступа по сети (localhost — без пароля)
- 🐳 Docker с GPU passthrough
- 🪟 Windows: один `.exe` со встроенным FFmpeg и UI
- 🔐 Подпись Windows-сборок через SignPath (при настроенных секретах)

## Как пользоваться

1. Откройте <http://localhost:8095>.
2. Выберите одно или несколько видео (или перетащите их в окно).
3. Выберите качество и нажмите **«🚀 Сжать»**.
4. Дождитесь надписи **«✅ Готово»** и скачайте файл.

Чтобы сжать целую папку, укажите путь к ней в блоке **«📁 Сжать папку»**.

## Как выбирается энкодер

При старте сервис проверяет каждый энкодер **реальным тестовым кадром**:

| Приоритет | Энкодеры | Условие |
|---|---|---|
| 1 | `hevc_nvenc`, `h264_nvenc` | NVIDIA + драйвер |
| 2 | `hevc_qsv`, `h264_qsv` | Intel Quick Sync |
| 3 | `hevc_amf`, `h264_amf` | AMD (Windows) |
| 4 | `libx265` | CPU |
| 5 | `libx264` | CPU fallback |

`FORCE_CPU=1` пропускает аппаратные энкодеры.
`ENCODER=hevc_qsv` (или другой) принудительно выбирает энкодер, если он проходит
тестовый кадр.

### Качество сжатия

| Режим | NVENC | QSV | AMF | CPU | CRF/CQ | Аудио |
|---|---|---|---|---|---|---|
| 🚀 `fast` | `p2` | `veryfast` | `speed` | `ultrafast` | 30 | 96k |
| ⚖️ `balance` | `p4` | `medium` | `balanced` | `medium` | 26 | 128k |
| 💎 `quality` | `p6` | `slow` | `quality` | `slow` | 20 | 192k |

В интерфейсе это кнопки **«Быстро»**, **«Баланс»** и **«Качество»**.
Старые значения `low`/`medium`/`high` понимаются автоматически.
Разрешение не меняется — сохраняется исходное; масштабирование можно задать
только через API/код (`Width`/`Height`).

Для CPU-кодирования потоки ограничены: 2 потока на 2–4 ядрах, 4 на 5–8,
8 на системах с большим числом ядер. Это не даёт `libx265` загрузить ПК на 100%.

## Быстрый старт (Linux, GPU)

Требования: NVIDIA GPU, драйвер, [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html).

```bash
git clone https://github.com/FuzzyHD1993/video-compressor.git
cd video-compressor
docker compose up -d --build
```

Откройте <http://localhost:8095>.

Проверка GPU:

```bash
docker run --rm --gpus all video-compressor:latest \
  ffmpeg -hide_banner -encoders | grep nvenc
```

### Docker и GPU

`docker-compose.yml` содержит оба способа проброса:

- `deploy.resources` — Docker + NVIDIA Container Toolkit;
- `devices: nvidia.com/gpu=all` — Podman/CDI.

```bash
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
docker compose logs -f
curl http://localhost:8095/health
```

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
| `BIND_ADDR` | `127.0.0.1` | Адрес прослушивания; `0.0.0.0` — доступ из сети |
| `BASIC_AUTH_USER` | `admin` | Логин Basic Auth |
| `BASIC_AUTH_HASH` | — | SHA-256 пароля; пусто — без пароля |
| `FORCE_CPU` | — | `1` — только CPU (`libx265`) |
| `ENCODER` | авто | Принудительный энкодер, например `hevc_qsv` |
| `MAX_WORKERS` | `2` (GPU) / 1–2 (CPU) | Сколько видео сжимать одновременно |
| `NO_BROWSER` | — | `1` — не открывать браузер |

### Доступ по сети и пароль

По умолчанию сервис слушает **только localhost** и не требует пароля.
Чтобы открыть его в локальной сети:

```bash
HASH='{SHA256}'$(printf '%s' 'мой-пароль' | sha256sum | cut -d' ' -f1)

BIND_ADDR=0.0.0.0 \
BASIC_AUTH_HASH="$HASH" \
./video-compressor
```

Проверка:

```bash
# без пароля → 401
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8095/health

# с паролем → 200
curl -s -u admin:мой-пароль -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8095/health
```

Поддерживается SHA-256 в hex или base64, с префиксом `{SHA256}` или без него.
Basic Auth передаёт пароль в base64 — используйте его только в доверенной сети,
через VPN или reverse proxy с HTTPS.

Docker по умолчанию слушает `0.0.0.0` внутри контейнера (иначе проброс порта
не работает); задайте `BASIC_AUTH_HASH` в `.env`/окружении для защиты.

## 📁 Сжать папку

Укажите папку с видео — программа найдёт все видеофайлы внутри (включая
вложенные папки), сожмёт их по одному. Исходные видео не удаляются.

- если поле «Куда сохранить» **пустое**, результат сохраняется **рядом с каждым
  исходным видео**;
- если папка указана — результаты складываются туда;
- Linux/Windows `.exe`: путь вида `/mnt/videos` или `C:\Videos`;
- Docker: путь **внутри контейнера** (смонтируйте папку в compose).

За один раз можно обработать до 1000 файлов.

### Куда сохраняются файлы

| Как запущено | Откуда файл | Куда результат |
|---|---|---|
| Native / `.exe` | загрузка из браузера | `output/` рядом с программой |
| Native / `.exe` | «Сжать папку» без папки назначения | рядом с исходным видео |
| Docker | любой способ | `/app/output/` (папка смонтирована на хосте) |

### Кнопка «Открыть папку»

- **Windows**: проводник с выделенным файлом;
- **macOS**: Finder;
- **Linux**: D-Bus `FileManager1` (Dolphin/Nautilus), затем `xdg-open`/`gio`;
- **Docker без GUI**: кнопка вернёт ошибку и покажет путь для копирования —
  используйте «Скачать».

## API

### `POST /api/jobs`

Загрузка одного или нескольких файлов:

```bash
curl -F "file=@one.mp4" -F "file=@two.mkv" -F "profile=balance" \
  http://localhost:8095/api/jobs
```

```json
{"jobs":[{"id":"job_...","filename":"one.mp4"}],"count":1,"jobId":"job_..."}
```

Старый `POST /upload` с полями `video`/`quality` поддерживается.

### `GET /api/jobs`

```json
[{
  "id":"job_...",
  "filename":"one.mp4",
  "status":"running",
  "progress":42,
  "outputName":"one_balance.mp4",
  "outputPath":"/app/output/one_balance.mp4",
  "profile":"balance",
  "inputSize":19578245,
  "outputSize":0,
  "createdAt":"2026-10-19T00:00:00Z"
}]
```

Статусы: `queued`, `running`, `done`, `error`, `canceled`.

### `DELETE /api/jobs/{id}`

Отменяет сжатие: убирает видео из списка или останавливает текущую обработку:

```bash
curl -X DELETE http://localhost:8095/api/jobs/job_...
```

### `POST /api/batch`

```bash
curl -X POST http://localhost:8095/api/batch \
  -H "Content-Type: application/json" \
  -d '{"input_dir":"/mnt/videos","output_dir":"/mnt/compressed","profile":"fast"}'
```

### Остальные

- `GET /status?jobId=...` — совместимость со старым API;
- `GET /download?jobId=...` — скачать результат (работает и для batch-файлов вне `output/`);
- `GET /download?file=...` — скачать файл из `output/`;
- `POST /api/open-folder` — `{jobId}` (любой готовый результат) или `{path}` (только внутри `output/`);
- `GET /health` — `{"status":"ok","encoder":"hevc_nvenc","version":"2.1.2"}`;
- `GET /` — веб-интерфейс.

## 🪟 Windows: один `.exe`

1. Скачайте `video-compressor.exe` из [Releases](https://github.com/FuzzyHD1993/video-compressor/releases).
2. Положите на флешку/диск и запустите.
3. Откройте <http://localhost:8095>.

FFmpeg с NVENC/QSV/AMF и веб-интерфейс встроены. Первый запуск — 5–10 секунд
(распаковка FFmpeg в `%TEMP%`). `temp/` и `output/` создаются рядом с `.exe`.
Для сжатия папки вводите Windows-путь, например `C:\Videos`.

### Сборка `.exe`

```bash
# ffmpeg.exe с NVENC/QSV/AMF (gyan.dev essentials) положить в cmd/server/
cp /path/to/ffmpeg.exe cmd/server/ffmpeg.exe

GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o video-compressor.exe ./cmd/server
```

Windows-сборка также описана в [docs/WINDOWS.md](docs/WINDOWS.md).

## Windows (WSL2)

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-video-compressor.ps1
```

## CI и релизы

Workflow [`.github/workflows/release.yml`](.github/workflows/release.yml):

1. скачивает FFmpeg с gyan.dev и проверяет SHA-256;
2. запускает `gofmt`, `go vet`, `go test`;
3. собирает Windows `.exe`;
4. отправляет в SignPath (если настроены секреты);
5. публикует GitHub Release с `.exe` и `SHA256SUMS`.

Теги с дефисом (`v2.0.0-rc1`) публикуются как prerelease.
Политика подписи: [docs/CODE_SIGNING.md](docs/CODE_SIGNING.md).

## Разработка

```bash
gofmt -l .
go vet ./...
go build ./...
go test ./...
```

```text
cmd/server/
  main.go        запуск, выбор энкодера, workers
  encoder.go     выбор ускорения, режимы качества, аргументы FFmpeg
  jobs.go        список видео, сжатие, прогресс, отмена, папки
  handlers.go    HTTP API и загрузка
  opener.go      открытие папки в Explorer/Finder/xdg-open
  static.go      встроенный UI
  static/index.html
  embed_windows.go / embed_other.go
docs/
  WINDOWS.md, CODE_SIGNING.md
.github/workflows/release.yml
Dockerfile, docker-compose.yml
```

## 🩺 Диагностика

| Проблема | Решение |
|---|---|
| Используется CPU вместо GPU | Проверьте драйвер, `/dev/dri`/`/dev/nvidia*`, `/health`; задайте `ENCODER=...`. |
| QSV не выбирается | Нужен Intel CPU с iGPU и драйвер; проверьте `ffmpeg -encoders \| grep qsv`. |
| AMF не выбирается | Только Windows + AMD GPU + драйвер. |
| Сжатие отменено, но FFmpeg жив | Обновите страницу; процесс завершится автоматически. |
| «Сжать папку» не находит файлы | Путь указывается на компьютере/в контейнере, а не в браузере. |
| «Открыть папку» не работает на Linux | Проверьте `DISPLAY`/`WAYLAND_DISPLAY`, наличие Dolphin/Nautilus и `dbus-send`/`xdg-open`. |
| «Открыть папку» пишет про Docker | Запрос попал в контейнер без GUI. Используйте native-запуск или кнопку «Скачать». |
| Порт занят | Задайте `PORT`. |
| `401 Unauthorized` | Задан `BASIC_AUTH_HASH`; передайте логин/пароль (`curl -u admin:...`). |
| Docker не видит GPU | `nvidia-ctk cdi generate`, проверьте capabilities `gpu,video`. |
| Windows SmartScreen | Неподписанная сборка; подпись появляется после настройки SignPath. |

## Ограничения

- Basic Auth защищает доступ, но без HTTPS пароль передаётся в base64;
  используйте доверенную сеть, VPN или reverse proxy с TLS.
- «Сжать папку» принимает произвольные пути компьютера — используйте только доверенный доступ.
- Потребительские GPU имеют лимит NVENC-сессий (по умолчанию 2 видео одновременно).
- CPU-кодирование медленнее; для слабых ПК выбирайте качество «Быстро».
- `.exe` не подписан, пока не настроены секреты SignPath.
