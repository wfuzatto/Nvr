# Formato de armazenamento v1

A gravação original é preservada sem reencode.

## Segmentos

- `.h264` para H.264 Annex-B
- `.h265` para H.265 Annex-B

NAL units usam start code `00 00 00 01`.

## Atomicidade

Durante gravação o arquivo termina em `.partial`. Após flush e fsync, ele é renomeado para o nome final. Apenas o arquivo finalizado entra na timeline.

## Índice

Cada diretório diário possui `index.jsonl`. Uma linha representa um segmento, evitando reescrever um índice grande a cada rotação.

## Integridade

O SHA-256 é calculado enquanto o segmento é escrito e armazenado no metadado final.

## Proteção

Um sidecar `.protected` impede remoção pela política de retenção.

## Playback derivado

```
Annex-B original
      |
      +--> evidence/export
      |
      +--> playback remux -> fMP4/HLS/WebRTC
```

A camada de playback será separada da ingestão para preservar o stream original.


## Índice de frames para playback

Novos segmentos possuem um sidecar:

```
arquivo.h264.frames.idx
arquivo.h265.frames.idx
```

O arquivo começa com header `NVFI` versionado e usa registros binários fixos de 20 bytes com offset, comprimento, RTP timestamp e flag de keyframe. O payload de vídeo não é duplicado.

A retenção remove esse sidecar junto com o segmento original. Gravações antigas sem sidecar continuam válidas como evidência, porém não entram automaticamente no playback HLS.
