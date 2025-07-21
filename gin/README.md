<div align="center">

![Monoscope's Logo](https://github.com/monoscope-tech/.github/blob/main/images/logo-white.svg?raw=true#gh-dark-mode-only)
![Monoscope's Logo](https://github.com/monoscope-tech/.github/blob/main/images/logo-black.svg?raw=true#gh-light-mode-only)

## Golang Gin SDK

[![Monoscope SDK](https://img.shields.io/badge/Monoscope-SDK-0068ff?logo=go)](https://github.com/topics/monoscope-sdk) [![Join Discord Server](https://img.shields.io/badge/Chat-Discord-7289da)](https://apitoolkit.io/discord?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme) [![Monoscope Docs](https://img.shields.io/badge/Read-Docs-0068ff)](https://apitoolkit.io/docs/sdks/golang?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme) [![GoDoc](https://godoc.org/github.com/monoscope-tech/monoscope-go?status.svg)](https://godoc.org/github.com/monoscope-tech/monoscope-go/main/tree/gin)

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
go get github.com/monoscope-tech/monoscope-go/gin
```

## Configuration

Next, set up your envrironment variables

```sh
OTEL_RESOURCE_ATTRIBUTES=at-project-key=<YOUR_API_KEY> # Your monoscope API key
OTEL_SERVICE_NAME="monoscope-otel-go-demo" # Service name for your the service you're integrating in
OTEL_SERVICE_VERSION="0.0.1" # Your application's service version
```

Then set it up in your project like so:

```go
package main

import (
	"log"

	monoscope "github.com/monoscope-tech/monoscope-go/gin"
	"github.com/gin-gonic/gin"
  _ "github.com/joho/godotenv/autoload"
)

func main() {
	shutdown, err := monoscope.ConfigureOpenTelemetry()
	if err != nil {
		log.Printf("error configuring openTelemetry: %v", err)
	}
	defer shutdown()

  r := gin.Default()

	// Add the monoscope gin middleware to monitor http requests
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

	r.GET("/greet/:name", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Hello " + c.Param("name")})
	})

	r.Run(":8000")
}
```

> [!IMPORTANT]
>
> To learn more configuration options (redacting fields, error reporting, outgoing requests, etc.), please read this [SDK documentation](https://apitoolkit.io/docs/sdks/golang/gin?utm_campaign=devrel&utm_medium=github&utm_source=sdks_readme).

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
