# Changelog

## 0.1.0 (2026-11-14)


### Features

* add the client, the request engine and every operation ([58718c3](https://github.com/flashpi1/flashpi-go/commit/58718c347f31e4da3e826b54e7cfdce34b843b33))
* add the errors ([9aa68e4](https://github.com/flashpi1/flashpi-go/commit/9aa68e418c7e66268500b5fca09f4eeb1ad1cc2d))
* generate the API's Go types from the OpenAPI contract ([c933d5b](https://github.com/flashpi1/flashpi-go/commit/c933d5bbf061afb4320dd0e47349fe5c37909349))
* give SendEmail an optional body and cap the size of answers ([5899642](https://github.com/flashpi1/flashpi-go/commit/5899642882ef04f9298efdc9e7ae267670d6a647))
* the Go SDK ([ec04407](https://github.com/flashpi1/flashpi-go/commit/ec044075346e47093078ebe0683b2b6fcd838fd1))
* verify the webhooks Flashpi sends ([81b0858](https://github.com/flashpi1/flashpi-go/commit/81b0858cca44a9ab1134e406c51af022435d1fd5))


### Bug fixes

* check the text of a body before encoding it, also on Go 1.27 ([11f85c1](https://github.com/flashpi1/flashpi-go/commit/11f85c1216e7d3b629b41c6b10ff389bc952b321))
* **openapi:** refuse member names that could end a struct tag ([85c03ce](https://github.com/flashpi1/flashpi-go/commit/85c03ce7da854a575e78ab2a387beac48fa4f902))
* **openapi:** type whole-number counters as int and pin every shared enum ([a89e4ca](https://github.com/flashpi1/flashpi-go/commit/a89e4cab3c6c70067f0ab2a38b90cebc74e30f39))
* read Problem Details only when their code is a string ([6a2a0d8](https://github.com/flashpi1/flashpi-go/commit/6a2a0d890cefceeeb4044bd65b8f7cbbbafdff25))
* refuse what cannot be sent as given, and never retry an answer that arrived ([861df83](https://github.com/flashpi1/flashpi-go/commit/861df835daa54e6666a2349e1554a6c7d6dc38e1))
