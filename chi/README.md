<div align="center">

![Monoscope's Logo](https://github.com/monoscope-tech/.github/blob/main/images/logo-white.svg?raw=true#gh-dark-mode-only)
![Monoscope's Logo](https://github.com/monoscope-tech/.github/blob/main/images/logo-black.svg?raw=true#gh-light-mode-only)

## Golang Chi SDK

[![Monoscope SDK](https://img.shields.io/badge/Monoscope-SDK-0068ff?logo=go)](https://github.com/topics/monoscope-sdk) [![Join Discord Server](https://img.shields.io/badge/Chat-Discord-7289da)](https://apitoolkit.io/discord?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme) [![Monoscope Docs](https://img.shields.io/badge/Read-Docs-0068ff)](https://apitoolkit.io/docs/sdks/golang?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme) [![GoDoc](https://godoc.org/github.com/monoscope-tech/monoscope-go?status.svg)](https://godoc.org/github.com/monoscope-tech/monoscope-go/main/tree/chi)

Monoscope is an end-to-end API and web services management toolkit for engineers and customer support teams. To integrate your Golang application with Monoscope, you need to use this SDK to monitor incoming traffic, aggregate the requests, and then deliver them to the Monoscope's servers.

</div>

---

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [Contributing and Help](#contributing-and-help)
- [License](#license)

---

## Installation

Kindly run the command below to install the SDK:

```sh
go get github.com/monoscope-tech/monoscope-go/chi
```

## Configuration

Next, set up your envrironment variables

```sh
OTEL_RESOURCE_ATTRIBUTES=at-project-key=<YOUR_API_KEY> # Your monoscope API key (required)
OTEL_SERVICE_NAME="monoscope-otel-go-demo" # Service name for your the service you're integrating in
OTEL_SERVICE_VERSION="0.0.1" # Your application's service version
```

Then set it up in your project like so:

```go
package main

import (
	"log"

	monoscope "github.com/monoscope-tech/monoscope-go/chi"
	"github.com/go-chi/chi/v5"
  _ "github.com/joho/godotenv/autoload"
)

func main() {
  shutdown, err := monoscope.ConfigureOpenTelemetry()
	if err != nil {
		log.Printf("error configuring openTelemetry: %v", err)
	}
	defer shutdown()

  r := chi.NewRouter()

	// Add the monoscope chi middleware to monitor http requests
	// And report errors to monoscope
	r.Use(monoscope.Middleware(monoscope.Config{
		Debug:               false,
		ServiceName:         "example-chi-server",
		ServiceVersion:      "0.0.1",
		Tags:                []string{"env:dev"},
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
		RedactHeaders:       []string{"Authorization", "X-Api-Key"},
		RedactRequestBody:   []string{"password", "credit_card"},
		RedactResponseBody:  []string{"password", "credit_card"},
	}))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, world!"))
	})

	if err := http.ListenAndServe(":8000", r); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
```

> [!IMPORTANT]
>
> To learn more configuration options (redacting fields, error reporting, outgoing requests, etc.), please read this [SDK documentation](https://apitoolkit.io/docs/sdks/golang/chi/).

## Contributing and Help

To contribute to the development of this SDK or request help from the community and our team, kindly do any of the following:

- Read our [Contributors Guide](https://github.com/monoscope-tech/.github/blob/main/CONTRIBUTING.md).
- Join our community [Discord Server](https://apitoolkit.io/discord?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme).
- Create a [new issue](https://github.com/monoscope-tech/monoscope-go/issues/new/choose) in this repository.

## License

This repository is published under the [MIT](LICENSE) license.

---

<div align="center">

<a href="https://apitoolkit.io?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme" target="_blank" rel="noopener noreferrer"><img src="https://github.com/monoscope-tech/.github/blob/main/images/icon.png?raw=true" width="40" /></a>

</div>
