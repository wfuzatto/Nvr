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
- **Media ingest/record core**: Go, sem CGO e sem runtime externo.
- **Decode/playback opcional futuro**: GStreamer/FFmpeg vendorizados quando necessários.
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


## Implementação atual

O desenvolvimento começou pelo **core offline-first**. Dependências de terceiros aceitas, como Pion WebRTC, ficam versionadas e vendorizadas no próprio repositório para que build e runtime não dependam da Internet.

Já disponível:

- servidor HTTP/API em Go;
- console web embutido no próprio binário;
- autenticação administrativa por token local;
- armazenamento atômico de câmeras em disco;
- credenciais RTSP e snapshot criptografadas com AES-GCM;
- CRUD de câmeras;
- probe RTSP nativo;
- sessão RTSP persistente DESCRIBE/SETUP/PLAY;
- autenticação RTSP Basic e Digest/MD5;
- RTP interleaved sobre TCP;
- H.264 e H.265/HEVC sem transcodificação;
- watchdog e reconexão automática;
- gravação segmentada com rename atômico;
- timeline e SHA-256 por segmento;
- retenção por dias e/ou bytes;
- proteção de evidência;
- pre-buffer em disco;
- snapshot HTTP Basic/Digest;
- frame broker não bloqueante para plugins;
- ONVIF WS-Discovery, Device/Media/Media2 e PTZ;
- provisionamento automático por profile ONVIF;
- playback HLS/MPEG-TS sem reencode;
- live HLS sob demanda, compartilhado por câmera e sem segunda conexão RTSP;
- WebRTC H.264 de baixa latência via Pion, compartilhado por câmera e sem transcodificação;
- RBAC local com Viewer, Operator, Supervisor e Admin;
- auditoria tamper-evident por hash-chain;
- mosaico live 1/4/9/16;
- timeline visual e exportação persistente de evidências;
- dashboard de saúde e endpoint Prometheus;
- fallback automático WebRTC → HLS;
- HLS.js vendorizado e embutido para Chrome/Android;
- health/readiness;
- unit tests;
- build Linux amd64/arm64;
- validação de testes/build com GOPROXY/GOSUMDB desligados e módulos em vendor/.

### Executar

```bash
go run ./cmd/nvr
```

No primeiro boot o NVR gera um token administrador e uma chave mestra em `./data`.

A interface fica disponível em `http://IP_DO_SERVIDOR:8080`.

### Verificar independência da Internet

```bash
make offline-check
```

Veja também `OFFLINE.md`.

> O media engine de gravação ainda não foi declarado como pronto. FFmpeg/GStreamer/PostgreSQL não serão exigidos até que seus artefatos offline façam parte do pacote da aplicação. Isso impede instalações parcialmente dependentes da Internet.


### Media Engine

A implementação atual do gravador está documentada em `docs/MEDIA_ENGINE.md` e o formato de arquivos em `docs/STORAGE_FORMAT.md`.

Os arquivos originais são gravados como elementary streams Annex-B. Playback web/remux será uma camada independente para não alterar a evidência original.


### ONVIF e playback

Consulte `docs/ONVIF.md`, `docs/PLAYBACK.md` e `docs/WEBRTC.md`.

O navegador nunca precisa conhecer usuário/senha da câmera. ONVIF resolve as URIs no servidor, e playback usa tokens temporários vinculados à câmera.
