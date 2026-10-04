# WebRTC de baixa latência v1

## Objetivo

WebRTC é a camada de live view de menor latência do NVR.

Ele não abre uma segunda conexão RTSP e não transcodifica vídeo.

Fluxo:

```
Câmera
  |
  | RTSP único
  v
Media Engine
  |
  +--> gravação
  |
  +--> Frame Broker
          |
          v
   WebRTC H.264 hub
          |
          +--> operador 1
          +--> operador 2
          +--> operador N
```

Um único hub por câmera compartilha o mesmo `TrackLocalStaticSample` entre PeerConnections simultâneas.

## Pilha

A implementação utiliza Pion WebRTC v4.2.22.

Pion é responsável por:

- ICE;
- SDP;
- DTLS;
- SRTP/SRTCP;
- RTP packetization;
- RTCP;
- lifecycle do PeerConnection.

O NVR não implementa criptografia WebRTC própria.

## Codec

Nesta versão o modo de baixa latência usa H.264.

A origem permanece exatamente o stream recebido da câmera:

- sem decoder;
- sem encoder;
- sem transcode;
- SPS/PPS são reaproveitados do SDP ou aprendidos do bitstream.

Quando a câmera está gravando em H.265/HEVC, o servidor responde que WebRTC sem transcodificação não está disponível e a interface troca automaticamente para live HLS.

Isso evita consumir GPU/CPU apenas para compatibilidade de navegador.

## Signaling

Endpoint autenticado:

`POST /api/v1/cameras/{id}/webrtc/session`

Entrada:

```json
{
  "type": "offer",
  "sdp": "v=0..."
}
```

Saída:

```json
{
  "session_id": "...",
  "type": "answer",
  "sdp": "v=0..."
}
```

Encerramento:

`DELETE /api/v1/webrtc/sessions/{session_id}`

Status:

`GET /api/v1/webrtc/status`

O signaling exige o token administrativo normal do NVR.

Depois da negociação, a mídia trafega por DTLS-SRTP.

## Rede local

Por padrão não há STUN ou TURN externo.

O navegador e o NVR usam candidatos ICE locais.

Configuração padrão:

```
NVR_WEBRTC_ENABLED=true
NVR_WEBRTC_UDP_PORT=50000
```

No firewall Linux, quando necessário:

```bash
sudo ufw allow 50000/udp
```

A porta pode ser alterada.

Se a porta UDP estiver ocupada ou a inicialização WebRTC falhar, o NVR não encerra: WebRTC fica indisponível e live HLS continua funcionando.

## Acesso por NAT / Internet

Se o servidor possuir um endereço público encaminhado para ele, configure:

```
NVR_WEBRTC_PUBLIC_IP=203.0.113.10
```

Também encaminhe essa porta UDP no roteador/firewall.

O NVR anuncia esse endereço como candidato ICE host mapeado.

Não há dependência de Google STUN, Cloudflare STUN ou qualquer outro serviço externo.

Em cenários de CGNAT/symmetric NAT onde não existe encaminhamento UDP direto, WebRTC poderá não estabelecer conexão. A interface então continua com HLS.

Um TURN próprio poderá ser adicionado futuramente como componente opcional e offline.

## Escala

A arquitetura evita um subscriber do Frame Broker por navegador e usa o ICE UDP mux do Pion para compartilhar uma única porta UDP entre todos os PeerConnections.

Para cada câmera ativa em WebRTC existe:

- 1 subscriber no Frame Broker;
- 1 track H.264 compartilhado;
- N PeerConnections.

Frames são packetizados uma vez pelo track e entregues aos bindings de todos os peers.

Hubs sem peers são removidos após período de inatividade.

## Segurança

- signaling protegido pelo Bearer token do NVR;
- mídia criptografada por DTLS-SRTP;
- nenhuma credencial RTSP é enviada ao navegador;
- nenhuma senha ONVIF é enviada ao navegador;
- nenhum servidor ICE externo é configurado por padrão;
- PeerConnections são removidas ao falhar/fechar ou quando o operador fecha o player.

## Offline

Pion e todas as suas dependências Go são gerados em `vendor/` pelo CI.

Após essa fabricação:

```bash
GOPROXY=off GOSUMDB=off GOFLAGS=-mod=vendor go test ./...
```

e os builds amd64/arm64 são executados sem resolução de módulos na Internet.

O servidor instalado utiliza somente o binário final.
