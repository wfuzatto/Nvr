# Media Engine v1

## Objetivo

O Media Engine v1 é o primeiro gravador real do NVR. Ele foi implementado dentro do core Go e usa somente a biblioteca padrão para preservar a exigência de instalação offline.

## Fluxo

```
Camera RTSP
   |
   | DESCRIBE / SETUP / PLAY
   | RTP interleaved over TCP
   v
RTSP Session
   |
   v
RTP Parser
   |
   v
H264/H265 Depacketizer
   |
   +------> Segment Recorder
   |
   +------> Encoded Frame Broker
```

O NVR abre uma conexão RTSP por câmera. Plugins não devem abrir uma segunda conexão com a câmera.

## RTSP

A implementação atual oferece RTSP/RTSPS, Basic, Digest/MD5, DESCRIBE, SETUP, PLAY, TEARDOWN, keepalive, RTP sobre TCP/interleaved, canal efetivamente negociado, watchdog, reconexão exponencial, H.264, H.265/HEVC e descarte de Access Units danificadas quando há perda de sequência RTP.

UDP não é usado nesta fase. Para redes municipais, RTP sobre TCP simplifica NAT/firewall e evita grande quantidade de portas UDP por câmera.

## Gravação

A gravação é feita sem transcodificação:

- H.264 -> Annex-B `.h264`
- H.265 -> Annex-B `.h265`

O recorder espera keyframe, injeta parameter sets do SDP quando disponíveis, escreve o bitstream original, gira em keyframe, grava primeiro como `.partial`, executa fsync, faz rename atômico e então registra no `index.jsonl`.

## Storage

```
recordings/
  <camera-id>/
    YYYY-MM-DD/
      HH-MM-SS.mmm_<unix-ns>.h264
      index.jsonl
```

## Timeline

Cada segmento contém ID, câmera, início, fim, codec, caminho, bytes, quantidade de keyframes e SHA-256.

## Pre-event

O pre-buffer é baseado em disco, não em RAM. A API retorna segmentos concluídos na janela e também o segmento `.partial` atualmente em gravação. Isso evita reservar dezenas de megabytes por câmera em RAM.

## Evidência

Um segmento pode ser protegido com sidecar `.protected`. A retenção nunca remove um segmento protegido.

Endpoint: `POST /api/v1/media/protect`.

## Retenção

| Variável | Padrão |
|---|---:|
| NVR_RETENTION_DAYS | 7 |
| NVR_STORAGE_MAX_BYTES | 0 |
| NVR_RETENTION_INTERVAL_SECONDS | 300 |
| NVR_SEGMENT_SECONDS | 60 |
| NVR_PRE_EVENT_SECONDS | 30 |
| NVR_RTSP_READ_TIMEOUT_SECONDS | 15 |

## Snapshots

A URL HTTP/HTTPS opcional de snapshot suporta Basic, Digest MD5 e Digest MD5-sess, limite de 12 MiB e validação de imagem. Credenciais ficam criptografadas.

## Frame Broker

O broker distribui Access Units codificadas localmente, com fila limitada, publicação não bloqueante e contador de drops. Plugin lento nunca bloqueia gravação.

## Limites conhecidos

Ainda não fazem parte desta fase: ONVIF discovery, decoder, JPEG derivado do RTSP, WebRTC, HLS, remux MP4/fMP4, exportação com corte exato, áudio gravado e RTP UDP/multicast.
