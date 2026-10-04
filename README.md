# NVR

Plataforma distribuída de videomonitoramento para operação em escala municipal, com arquitetura extensível por plugins.

## Objetivos

- Ingestão de câmeras IP via RTSP/ONVIF.
- Gravação contínua e por eventos com retenção configurável.
- Live view de baixa latência.
- Operação distribuída por cidade, região e site.
- Continuidade local mesmo sem conectividade com o servidor central.
- Plugins de análise de vídeo, OCR/LPR, detecção, notificações e integrações.
- API pública e contratos internos versionados.
- Auditoria, RBAC, trilha de evidências e controles de privacidade.
- Observabilidade nativa.

## Arquitetura-base

O sistema é dividido em dois planos:

1. **Control Plane**: cadastro, autenticação, configuração, políticas, pesquisa, auditoria, inventário e orquestração.
2. **Media/Edge Plane**: conexão com câmeras, gravação, análise, live view e execução de plugins perto da origem do vídeo.

O vídeo não deve atravessar o servidor central sem necessidade. Cada nó de edge mantém gravação local e envia ao control plane apenas metadados/eventos; live view e exportações são encaminhados sob demanda.

## Stack escolhida

- **SO de produção**: Ubuntu Server 26.04 LTS.
- **Containers**: Docker + Compose por nó na primeira fase; imagens OCI compatíveis com futura orquestração.
- **Control plane / node agent**: Go.
- **Media engine**: Rust + GStreamer.
- **Utilitários de mídia**: FFmpeg.
- **Frontend**: TypeScript + React.
- **Plugins de IA**: linguagem livre; referência em Python para ML/CV.
- **API externa**: REST/OpenAPI.
- **RPC interno**: gRPC + Protobuf.
- **Event bus**: NATS + JetStream.
- **Banco**: PostgreSQL + PostGIS.
- **Vídeo/evidências**: filesystem local + backend S3 compatível.
- **Observabilidade**: OpenTelemetry + Prometheus-compatible metrics.

## Repositórios

- `Nvr`: plataforma principal.
- `Plate_ocr`: primeiro plugin oficial, responsável por OCR/LPR de placas.

## Princípio de plugins

Plugins não recebem credenciais RTSP de câmeras. O NVR abre cada stream uma vez e distribui frames/metadados aos plugins conforme o contrato solicitado. Isso reduz conexões, banda, decodificação duplicada e impacto nas câmeras.

Consulte:
- `docs/ARCHITECTURE.md`
- `docs/PLUGIN_ARCHITECTURE.md`
- `docs/ROADMAP.md`
- `docs/ADR/0001-platform-stack.md`

## Status

Fase 0 — arquitetura e contratos.
