# Playback HLS v1

## Princípio

Playback é uma camada derivada. A gravação original Annex-B continua sendo a fonte de evidência e não é reencodada.

## Sidecar de frames

Novos segmentos possuem:

```
arquivo.h264
arquivo.h264.frames.jsonl
```

Cada linha do sidecar registra:

- offset;
- length;
- RTP timestamp;
- keyframe;
- horário de recepção.

O sidecar é pequeno e não duplica o payload de vídeo.

## HLS

Fluxo:

```
Annex-B + frame index
        |
        v
MPEG-TS remux on demand
        |
        v
HLS playlist
        |
        +--> HLS.js local / MSE
        +--> HLS nativo
```

Não há transcodificação.

## Segurança

A interface autenticada solicita:

`POST /api/v1/cameras/{id}/playback/session`

O NVR devolve uma playlist com token HMAC temporário, vinculado ao camera ID.

O token:

- expira;
- não contém a senha administrativa;
- não pode ser reutilizado para outra câmera.

Playlist e segmentos podem então ser lidos pelo elemento de vídeo/HLS.js sem Authorization header.

## Runtime web offline

HLS.js 1.7.3 é fixado por versão.

O CI baixa exclusivamente o release fixado, verifica o SHA-256 do ZIP e grava `hls.min.js` no repositório. A licença Apache 2.0 também acompanha o projeto.

Em produção nenhum CDN é acessado.

## Gravações antigas

Segmentos criados antes do frame sidecar continuam válidos para retenção/evidência, mas não são anunciados na playlist HLS.

## Codec

O muxer suporta H.264 e H.265 em MPEG-TS. A reprodução efetiva de H.265 depende do codec suportado pelo navegador/dispositivo cliente.

## Timestamp

O mux usa o RTP clock registrado no segmento. Entre segmentos é emitido `#EXT-X-DISCONTINUITY`, permitindo reinício seguro da timeline após segmentação/reconexão.
