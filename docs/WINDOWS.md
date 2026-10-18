# 🪟 Установка на Windows (WSL2 + GPU)

> Проверено на уровне конфигурации и скриптов. Финальная проверка GPU требует
> реального Windows-хоста с NVIDIA GPU.

## 0. Самый простой вариант — один `.exe`

Скачайте `video-compressor.exe` из
[Releases](https://github.com/FuzzyHD1993/video-compressor/releases), положите
на флешку и запустите двойным кликом. FFmpeg с NVENC и веб-интерфейс уже
встроены; Docker, WSL2 и установка FFmpeg не нужны.

- Требования: Windows 10/11 x64 + драйвер NVIDIA.
- Первый запуск: 5–10 секунд (распаковка FFmpeg в `%TEMP%`).
- Результаты: папка `output/` рядом с `.exe`.
- Открыть: <http://localhost:8095>.

Разделы ниже описывают альтернативную установку через WSL2/Docker.

## 1. Требования

| Компонент | Требование |
|---|---|
| Windows | Windows 11 (или Windows 10 21H2+) |
| GPU | NVIDIA GTX 1660 SUPER или новее |
| Драйвер | Windows-драйвер NVIDIA **с поддержкой WSL** |
| WSL | WSL2 + Ubuntu 24.04 |
| Контейнеры | Docker Desktop (WSL2 backend) или Podman Desktop |

⚠️ **Не устанавливайте Linux-драйвер NVIDIA внутри WSL2.** WSL использует
Windows-драйвер; GPU пробрасывается в дистрибутив как `/dev/dxg`, а не
`/dev/nvidia0`.

## 2. WSL2 и Ubuntu

```powershell
wsl --install -d Ubuntu-24.04
wsl --update
wsl --status
```

Проверка GPU внутри WSL:

```bash
nvidia-smi
ls -l /dev/dxg
```

Если `nvidia-smi` не найден или GPU не виден — обновите Windows-драйвер NVIDIA
(версия с поддержкой WSL) и перезагрузитесь.

### CUDA toolkit (опционально)

Для NVENC **не требуется** CUDA toolkit — достаточно драйвера и FFmpeg.
Устанавливайте toolkit только если он нужен для других задач:

```bash
wget https://developer.download.nvidia.com/compute/cuda/repos/wsl-ubuntu/x86_64/cuda-keyring_1.1-1_all.deb
sudo dpkg -i cuda-keyring_1.1-1_all.deb
sudo apt-get update
sudo apt-get -y install cuda-toolkit-13-2
```

## 3. Вариант A — Docker Desktop (рекомендуется)

1. Установите Docker Desktop и включите **Settings → General → Use the WSL 2 based engine**.
2. В **Settings → Resources → WSL Integration** включите интеграцию с Ubuntu.
3. В WSL проверьте:

```bash
docker --version
docker run --rm --gpus all ubuntu nvidia-smi
```

4. Установите приложение:

```bash
git clone https://github.com/FuzzyHD1993/video-compressor.git
cd video-compressor
docker compose up -d --build
```

5. Проверьте NVENC в контейнере:

```bash
docker exec nas-video-compressor ffmpeg -hide_banner -encoders | grep nvenc
```

Ожидаемый вывод: `hevc_nvenc`, `h264_nvenc`.

## 4. Вариант B — Podman Desktop + CDI

Podman на Windows работает внутри отдельной VM (`podman machine`), поэтому
NVIDIA Container Toolkit и CDI-спецификация должны быть созданы **внутри этой
VM**, а репозиторий — доступен внутри неё.

```bash
podman machine ssh

# Внутри podman machine:
sudo dnf install -y nvidia-container-toolkit
sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
nvidia-ctk cdi list
# Ожидание: nvidia.com/gpu=0

exit
```

Запуск:

```bash
podman compose up -d --build
```

`docker-compose.yml` уже содержит CDI-устройство:

```yaml
devices:
  - nvidia.com/gpu=all
security_opt:
  - label=disable
```

Проверка GPU:

```bash
podman run --rm --device nvidia.com/gpu=all --security-opt=label=disable \
  ubuntu nvidia-smi
```

### Если Podman не видит репозиторий

Склонируйте его внутри VM:

```bash
podman machine ssh 'git clone https://github.com/FuzzyHD1993/video-compressor.git ~/video-compressor'
podman machine ssh 'cd ~/video-compressor && podman compose up -d --build'
```

## 5. Автоматический установщик

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-video-compressor.ps1                 # Docker Desktop
.\scripts\install-video-compressor.ps1 -Runtime podman # Podman
```

Скрипт проверяет WSL2, наличие GPU, контейнерный runtime, клонирует или
обновляет репозиторий и запускает compose.

## 6. Доступ

Откройте <http://localhost:8095>. Файлы результатов сохраняются в
`~/video-compressor/output` (Linux/WSL) или в volume контейнера.

## 7. Диагностика

```bash
# GPU в WSL2
nvidia-smi
ls -l /dev/dxg

# GPU в контейнере
docker run --rm --gpus all ubuntu nvidia-smi

# NVENC в ffmpeg контейнера
docker exec nas-video-compressor ffmpeg -hide_banner -encoders | grep -E 'nvenc|amf'

# Логи приложения
docker logs -f nas-video-compressor
```

| Проблема | Решение |
|---|---|
| `nvidia-smi` не работает в WSL | Обновить Windows-драйвер NVIDIA, `wsl --update` |
| `--gpus all` не работает | Установить/обновить Docker Desktop, включить WSL2 backend |
| `nvenc` отсутствует в контейнере | Проверить образ: FFmpeg должен быть собран с `--enable-nvenc` |
| Podman: `CDI device injection failed` | `nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml` внутри `podman machine` |
| Нет GPU-ускорения, но всё работает | Проверить логи: при отсутствии NVENC используется `libx265` |
