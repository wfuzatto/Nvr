# ADR 0002 — Media ingest nativo e offline

Status: Accepted

Supersede parcialmente: `ADR 0001 — Stack de plataforma`, apenas na decisão sobre o caminho crítico do Media Engine.

## Contexto

O requisito operacional passou a exigir que o repositório contenha tudo que é necessário para instalar e executar o NVR sem Internet.

A primeira implementação provou que ingestão RTSP, RTP interleaved, depacketização H.264/H.265, watchdog, gravação segmentada, timeline e retenção podem ser executados pelo mesmo runtime Go do core, sem dependências nativas externas.

## Decisão

O caminho crítico de gravação passa a ser implementado em Go:

```
RTSP
 -> RTP/TCP
 -> H264/H265 depacketizer
 -> Annex-B recorder
 -> timeline
```

O processo de gravação não depende de:

- FFmpeg;
- GStreamer;
- Python;
- Node.js;
- Docker;
- CGO.

As releases são compiladas com `CGO_ENABLED=0`.

## GStreamer e FFmpeg

Não foram descartados.

Eles passam a ser runtimes opcionais para capacidades em que agregam valor real:

- decode por hardware;
- conversão de pixel format;
- frame extraction;
- remux fMP4;
- HLS;
- WebRTC;
- exportação de clips;
- compatibilidade com codecs adicionais.

Antes de qualquer um se tornar requisito, seus binários, bibliotecas, licenças e hashes precisam fazer parte do pacote offline.

## Rust

Rust deixa de ser obrigatório para o Media Engine inicial.

Poderá ser introduzido em componentes específicos somente se profiling demonstrar ganho material que justifique um segundo toolchain/runtime.

## Consequências

Vantagens:

- um único binário para ingestão e control core;
- instalação sem runtime adicional;
- menor superfície de atualização;
- menos processos;
- menor risco de incompatibilidade de bibliotecas nativas;
- cross-build amd64/arm64 simples.

Tradeoffs:

- protocolos/codecs precisam ser implementados e testados por nós;
- aceleração GPU de decode ainda exige camada adicional;
- playback web exigirá remux/empacotamento futuro;
- formatos raros podem exigir adapter externo.

## Regra

A gravação original deve continuar funcionando mesmo que todos os runtimes opcionais de decode, IA e playback estejam indisponíveis.
