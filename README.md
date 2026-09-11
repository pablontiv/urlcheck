# urlcheck

CLI pequeña en Go para responder: **¿qué servicios de esta lista puedo usar desde mi red?**

Lee URLs desde un archivo plano (estilo hosts / blocklist) y comprueba si responden, si el DNS las hunde o si el servidor contesta 403/451.

No incluye ni recomienda listas de terceros. Tú pones las URLs.

## Formato del archivo

`services.txt` (o el que pases con `-list`):

```
# comentarios con #
Google          https://www.google.com
Netflix         https://www.netflix.com
Mi portal       https://ejemplo.tld/login
```

También vale:

```
Nombre | https://ejemplo.tld/path
https://solo-una-url.tld
```

## Uso

```bash
go run .                         # usa services.txt
go run . -list mis_urls.txt
go run . -timeout 5s -workers 20
go run . -json                   # salida máquina
go run . -insecure               # TLS con cert raro
go run . -method GET
```

Compilar:

```bash
go build -o urlcheck .
./urlcheck -list services.txt
```

## Qué significa cada estado

| Estado    | Criterio |
|-----------|----------|
| **OK**    | HTTP 2xx/3xx (o 401: el host vive y pide login) |
| **BLOCKED** | DNS a loopback/sinkhole, HTTP 451 o 403 |
| **DOWN**  | timeout, conexión rechazada, 5xx, DNS roto |
| **ERROR** | TLS u otro fallo |

Un 403 no siempre es censura (puede ser WAF). Un timeout tampoco: el servidor puede estar caído. La herramienta reporta lo que ve **desde tu red actual**.

## Notas

- Solo comprueba alcanzabilidad HTTP(S). No valida usuarios, tokens ni listas M3U.
- Respeta `HTTP_PROXY` / `HTTPS_PROXY` si están definidos.
- HEAD primero; si el servidor no lo acepta, reintenta con GET.
