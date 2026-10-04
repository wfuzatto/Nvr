# Playback HLS v1

## Princípio

Playback é uma camada derivada. A gravação original Annex-B continua sendo a fonte de evidência e não é reencodada.

## Sidecar de frames

Novos segmentos possuem:

```
arquivo.h264
arquivo.h264.frames.idx
```

O sidecar usa registros binários fixos de 20 bytes. Cada registro contém:

- offset de 64 bits;
- comprimento de 32 bits;
- RTP timestamp de 32 bits;
- flag de keyframe;
- bytes reservados para evolução do formato.

Há um header versionado `NVFI`. O sidecar não duplica o payload de vídeo e foi desenhado para manter overhead baixo em instalações com milhares de câmeras.

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


## Ao vivo HLS

O live view HLS reutiliza o mesmo Frame Broker alimentado pela conexão RTSP de gravação.

Fluxo:

```
RTSP único
   |
Media Engine
   |
Frame Broker
   |
Live HLS hub sob demanda
   |
MPEG-TS em memória
   |
HLS.js / HLS nativo
```

Características:

- não abre uma segunda conexão RTSP;
- não reencoda;
- um hub é compartilhado entre vários visualizadores da mesma câmera;
- segmentos são iniciados em keyframe;
- alvo de aproximadamente 2 segundos, limitado pelo GOP real da câmera;
- ring em memória de 8 segmentos;
- limite de 32 MiB/15 segundos para impedir GOP defeituoso de consumir memória sem limite;
- hub é encerrado após período sem visualizadores;
- playlist e segmentos usam token HMAC temporário vinculado à câmera.

WebRTC permanece planejado para a camada de baixa latência. HLS é o fallback universal e também atende playback gravado.
