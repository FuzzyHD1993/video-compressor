# 🎬 Video Compressor

Локальный веб-сервис для сжатия видео через FFmpeg. По умолчанию использует
GPU (NVIDIA NVENC), при отсутствии аппаратного энкодера автоматически
переключается на CPU (`libx265`).

## Возможности

- 🚀 HEVC/H.264 через NVENC (`hevc_nvenc` / `h264_nvenc`)
- 🖥 Автоматический fallback на CPU (`libx265`)
- 🎚 Три профиля качества: 1080p / 720p / 480p
- 📊 Прогресс обработки в реальном времени
- 🐳 Docker-образ с FFmpeg и GPU passthrough
- 🪟 Установка на Windows через WSL2 (Docker Desktop или Podman)

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

## Запуск без Docker

```bash
sudo apt install ffmpeg        # или pacman -S ffmpeg
go build -o video-compressor ./cmd/server
./video-compressor
```

Сервер слушает `http://localhost:8080`.

### Переменные окружения

| Переменная | По умолчанию | Описание |
|---|---|---|
| `PORT` | `8080` | HTTP-порт |
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

## Windows (WSL2)

Подробная инструкция: [docs/WINDOWS.md](docs/WINDOWS.md).

Быстрый вариант (PowerShell, Docker Desktop с WSL2 backend):

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-video-compressor.ps1
```

## API

- `POST /upload` — `multipart/form-data`: `video` (файл), `quality` (`high`/`medium`/`low`); возвращает `{"jobId": "..."}`
- `GET /status?jobId=...` — состояние задачи
- `GET /` — веб-интерфейс

## Разработка

```bash
gofmt -l .
go vet ./...
go build ./...
go test ./...
```

## Ограничения

- GTX 1660 SUPER (Turing) поддерживает H.264/HEVC NVENC, но **не AV1**.
- Потребительские GPU имеют лимит одновременных NVENC-сессий, поэтому по
  умолчанию запускается не более двух задач.
- Linux-драйвер NVIDIA внутри WSL2 устанавливать не нужно — используется
  драйвер Windows, проброшенный через `/dev/dxg`.
