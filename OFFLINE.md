# Política offline do NVR

## Regra

O NVR não pode precisar da Internet para instalar, iniciar, gravar, visualizar ou executar plugins já instalados. A rede operacional das câmeras e dos nós é permitida; Internet não é pré-requisito.

## Core atual

O primeiro core foi deliberadamente implementado apenas com a biblioteca padrão do Go:

- nenhum `go get`;
- nenhum módulo Go de terceiro;
- nenhum npm ou Node.js;
- nenhum CDN;
- frontend HTML/CSS/JS embutido no binário;
- persistência local atômica;
- AES-GCM para segredos RTSP;
- autenticação por token local.

O comando `make offline-check` executa testes com `GOPROXY=off` e `GOSUMDB=off`.

## Regra para novas dependências

Uma dependência só pode entrar se todos os artefatos necessários para build/deploy offline também fizerem parte do pacote do projeto.

Para dependências binárias futuras:

```
runtime/
  manifest.json
  linux-amd64/
  linux-arm64/
```

O manifesto deve registrar versão, origem, licença e SHA-256.

Scripts de instalação/produção não podem depender de:

- `curl`/`wget` para baixar componentes;
- `apt install` apontando para Internet;
- `docker pull`;
- `go get`;
- `npm install` em registry remoto;
- `pip install` em PyPI;
- `git clone` externo.

## Docker

Docker pode ser um formato opcional de empacotamento, mas não é requisito do core. Se usarmos containers, as imagens OCI/tar deverão acompanhar o pacote offline e serão carregadas localmente.

## Media engine e banco

O projeto não vai fingir que FFmpeg, GStreamer ou PostgreSQL estão disponíveis se seus binários ainda não estiverem vendorizados. Nesta etapa o control core usa store local atômica e a camada de mídia será adicionada com runtime totalmente offline.

## Release

CI com Internet pode fabricar o pacote, mas o artefato final precisa passar por teste com resolução de dependências e saída para Internet desabilitadas.
