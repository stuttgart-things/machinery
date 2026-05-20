# [1.10.0](https://github.com/stuttgart-things/machinery/compare/v1.9.0...v1.10.0) (2026-05-20)


### Features

* **examples:** gRPC consumer CLI for GRPCRoute access patterns ([#57](https://github.com/stuttgart-things/machinery/issues/57)) ([1187940](https://github.com/stuttgart-things/machinery/commit/118794031c773987b4691290340b261f56ca1573)), closes [stuttgart-things/argocd#184](https://github.com/stuttgart-things/argocd/issues/184)

# [1.9.0](https://github.com/stuttgart-things/machinery/compare/v1.8.0...v1.9.0) (2026-05-20)


### Features

* **kcl:** Secret-mount auth token ([#70](https://github.com/stuttgart-things/machinery/issues/70)) ([#71](https://github.com/stuttgart-things/machinery/issues/71)) ([1a49f60](https://github.com/stuttgart-things/machinery/commit/1a49f602f24ae450f5009b54cae123eca21d2c2f))

# [1.8.0](https://github.com/stuttgart-things/machinery/compare/v1.7.0...v1.8.0) (2026-05-20)


### Features

* **auth:** bearer-token gRPC interceptor ([#56](https://github.com/stuttgart-things/machinery/issues/56)) ([#69](https://github.com/stuttgart-things/machinery/issues/69)) ([ae03bcc](https://github.com/stuttgart-things/machinery/commit/ae03bcc73477c5cd67002ccc820a14825de916c3))

# [1.7.0](https://github.com/stuttgart-things/machinery/compare/v1.6.1...v1.7.0) (2026-05-20)


### Features

* **kcl:** GRPCRoute for in-cluster gRPC access ([#68](https://github.com/stuttgart-things/machinery/issues/68)) ([490fba0](https://github.com/stuttgart-things/machinery/commit/490fba096388b9e61cfb22ec02120419d413d7f4)), closes [#55](https://github.com/stuttgart-things/machinery/issues/55) [argocd#161](https://github.com/argocd/issues/161)

## [1.6.1](https://github.com/stuttgart-things/machinery/compare/v1.6.0...v1.6.1) (2026-05-19)


### Bug Fixes

* **web:** drive filter request via htmx.ajax, not htmx.trigger ([23ac318](https://github.com/stuttgart-things/machinery/commit/23ac31844fef921193f071fa283b48083afeb4a7))

# [1.6.0](https://github.com/stuttgart-things/machinery/compare/v1.5.5...v1.6.0) (2026-05-19)


### Features

* **web:** multi-select kind+namespace filters, fix stale-poll race ([#65](https://github.com/stuttgart-things/machinery/issues/65)) ([b1d7f0b](https://github.com/stuttgart-things/machinery/commit/b1d7f0befd5476f33dc49db57f551d6957a68b09))

## [1.5.5](https://github.com/stuttgart-things/machinery/compare/v1.5.4...v1.5.5) (2026-05-19)


### Bug Fixes

* tolerate per-kind 404s in GetResources, bump ExternalSecret example to v1 ([#66](https://github.com/stuttgart-things/machinery/issues/66)) ([56d9c28](https://github.com/stuttgart-things/machinery/commit/56d9c288ff2c5f30a87a01f0dc81f850fa621597))

## [1.5.4](https://github.com/stuttgart-things/machinery/compare/v1.5.3...v1.5.4) (2026-05-19)


### Bug Fixes

* surface Gateway API status + render slice info fields ([#63](https://github.com/stuttgart-things/machinery/issues/63)) ([65bc7c6](https://github.com/stuttgart-things/machinery/commit/65bc7c6801f0737c3101c39aecee09dc4ac8757a))

## [1.5.3](https://github.com/stuttgart-things/machinery/compare/v1.5.2...v1.5.3) (2026-05-19)


### Bug Fixes

* **ci:** publish PR image to GHCR with pr-<num>-<sha> tag ([73bb943](https://github.com/stuttgart-things/machinery/commit/73bb943cd9306b5126213ead8c3f3b972a685647))

## [1.5.2](https://github.com/stuttgart-things/machinery/compare/v1.5.1...v1.5.2) (2026-05-19)


### Bug Fixes

* **deps:** update kubernetes monorepo to v0.36.1 ([a1fbabc](https://github.com/stuttgart-things/machinery/commit/a1fbabcb36879f7abe85c128241c827995b0ea89))

## [1.5.1](https://github.com/stuttgart-things/machinery/compare/v1.5.0...v1.5.1) (2026-05-19)


### Bug Fixes

* **kcl:** mount MACHINERY_CONFIG as a file instead of injecting contents ([29a8ca4](https://github.com/stuttgart-things/machinery/commit/29a8ca4179e8dcf261fb5b9cee7b249fcb3b1892)), closes [#52](https://github.com/stuttgart-things/machinery/issues/52)

# [1.5.0](https://github.com/stuttgart-things/machinery/compare/v1.4.2...v1.5.0) (2026-05-18)


### Features

* add PR-preview CI workflows ([4005f21](https://github.com/stuttgart-things/machinery/commit/4005f21f3e1fdf853b8b15a1621baef092d7bb59))

## [1.4.2](https://github.com/stuttgart-things/machinery/compare/v1.4.1...v1.4.2) (2026-05-18)


### Bug Fixes

* **e2e:** extract port-forward into bash script for mvdan/sh compatibility ([b96e4f9](https://github.com/stuttgart-things/machinery/commit/b96e4f989d093ac63b402611678dafa93a7cf5f3))

## [1.4.1](https://github.com/stuttgart-things/machinery/compare/v1.4.0...v1.4.1) (2026-05-18)


### Bug Fixes

* **e2e:** pre-warm kcl module cache so first run doesn't pollute stdout ([a1d1d78](https://github.com/stuttgart-things/machinery/commit/a1d1d7889a17ea14e6cf2d25ab16b30cae9b1cc8))

# [1.4.0](https://github.com/stuttgart-things/machinery/compare/v1.3.2...v1.4.0) (2026-05-18)


### Features

* add kind-based e2e job ([00844cc](https://github.com/stuttgart-things/machinery/commit/00844cc909bda2c6179bf2438384b6b152e24380))

## [1.3.2](https://github.com/stuttgart-things/machinery/compare/v1.3.1...v1.3.2) (2026-05-18)


### Bug Fixes

* **deps:** update module google.golang.org/grpc to v1.81.1 ([d2d9403](https://github.com/stuttgart-things/machinery/commit/d2d94037015de20a1c5b10805778f03eda9b1c06))

## [1.3.1](https://github.com/stuttgart-things/machinery/compare/v1.3.0...v1.3.1) (2026-05-18)


### Bug Fixes

* **deps:** update module google.golang.org/grpc to v1.79.3 [security] ([bb580c1](https://github.com/stuttgart-things/machinery/commit/bb580c1ed92225ffb3382033fb947be9a91e6e5b))

# [1.3.0](https://github.com/stuttgart-things/machinery/compare/v1.2.1...v1.3.0) (2026-04-10)


### Features

* rework README and align footer with clusterbook branding ([a9c58a2](https://github.com/stuttgart-things/machinery/commit/a9c58a22e0c3de91014d23df8260117e7759df24))

## [1.2.1](https://github.com/stuttgart-things/machinery/compare/v1.2.0...v1.2.1) (2026-04-10)


### Bug Fixes

* add docs directory for pages deployment ([0c0f6e1](https://github.com/stuttgart-things/machinery/commit/0c0f6e17ec10fd25fb23a9b3509d45a9275664db))

# [1.2.0](https://github.com/stuttgart-things/machinery/compare/v1.1.0...v1.2.0) (2026-04-10)


### Features

* add build info footer and favicon ([0881ecd](https://github.com/stuttgart-things/machinery/commit/0881ecd3a955c0abbf12d2a535d0a00967f3636d)), closes [#43](https://github.com/stuttgart-things/machinery/issues/43) [#44](https://github.com/stuttgart-things/machinery/issues/44)

# [1.1.0](https://github.com/stuttgart-things/machinery/compare/v1.0.0...v1.1.0) (2026-04-09)


### Bug Fixes

* downgrade go directive to 1.25.0 for CI compatibility ([e02b744](https://github.com/stuttgart-things/machinery/commit/e02b744ab3df84d403deb8ebc648b300aab780d8))


### Features

* add infoFields config and clickable detail view ([9adf2e6](https://github.com/stuttgart-things/machinery/commit/9adf2e692c31f8ed6531abd84a15fba8768e1140)), closes [stuttgart-things/machinery#45](https://github.com/stuttgart-things/machinery/issues/45)

# 1.0.0 (2026-03-14)


### Bug Fixes

* add .releaserc for semantic-release ([d22c67d](https://github.com/stuttgart-things/machinery/commit/d22c67d596f1103f582e5b215378cef99da636ad))
* correct KCL conditional list syntax in deploy and service ([23e4869](https://github.com/stuttgart-things/machinery/commit/23e48698fc4cfa620c3be4ce5379c66104bb0049))
* **deps:** update module google.golang.org/grpc to v1.79.2 ([e598bba](https://github.com/stuttgart-things/machinery/commit/e598bbadd960eebf277f3c16cc3f99c61d92d784))
* **deps:** update module google.golang.org/protobuf to v1.36.11 ([3eced39](https://github.com/stuttgart-things/machinery/commit/3eced391cc277ea577f3a67bf28860d73a60d24a))
* **deps:** update module google.golang.org/protobuf to v1.36.6 ([af89e62](https://github.com/stuttgart-things/machinery/commit/af89e622c395bda0644c8cb135f9cee8524699f9))
* handle ignored error return values from unstructured field access ([79fd231](https://github.com/stuttgart-things/machinery/commit/79fd231ccc2fde37d79af573d998e03d98d6d233)), closes [#13](https://github.com/stuttgart-things/machinery/issues/13)
* remove hardcoded InsecureSkipVerify from TLS config ([70609db](https://github.com/stuttgart-things/machinery/commit/70609db6aa9692da5e325800a237221794989206)), closes [#10](https://github.com/stuttgart-things/machinery/issues/10)
* use correct kcl_options format in deploy profile ([3cdef72](https://github.com/stuttgart-things/machinery/commit/3cdef721f9df1c844536d312cefc3d81f0926588))


### Features

* add Backstage catalog-info.yaml and comprehensive README ([a2ee33e](https://github.com/stuttgart-things/machinery/commit/a2ee33ee252a9e6deba8046f13649d12579e1f0f)), closes [#21](https://github.com/stuttgart-things/machinery/issues/21) [#20](https://github.com/stuttgart-things/machinery/issues/20)
* add GitHub Actions CI/CD workflows ([f64a091](https://github.com/stuttgart-things/machinery/commit/f64a09194d1d8bc032f24b8cd04af69f7fcd188f)), closes [#19](https://github.com/stuttgart-things/machinery/issues/19) [#9](https://github.com/stuttgart-things/machinery/issues/9)
* add HTMX web frontend for resource browsing ([3536e0c](https://github.com/stuttgart-things/machinery/commit/3536e0ca62282f857c3ca5834c9b8c585060bfdb)), closes [#16](https://github.com/stuttgart-things/machinery/issues/16)
* add KCL-based Kubernetes deployment manifests ([d7e15d5](https://github.com/stuttgart-things/machinery/commit/d7e15d57081aa2f56b86e2add2a1aacad7addfa9)), closes [#18](https://github.com/stuttgart-things/machinery/issues/18)
* add unit tests for config, server, and helper functions ([bda177f](https://github.com/stuttgart-things/machinery/commit/bda177f8aa8e14b2907b96f05f2f24918d721420)), closes [#8](https://github.com/stuttgart-things/machinery/issues/8)
* main ([a73422e](https://github.com/stuttgart-things/machinery/commit/a73422e66587c924fb0a731a3052a6ed52a8b8bd))
* main ([accdc68](https://github.com/stuttgart-things/machinery/commit/accdc6813eb6bf2495fe1de13197fe8055832369))
* make resource watching generic with configurable status fields ([2f9d33b](https://github.com/stuttgart-things/machinery/commit/2f9d33bc548ee6e24842c7dbd61e5e5edba55ec5))
* structured logging, graceful shutdown, validation, configurable resources ([a699d6a](https://github.com/stuttgart-things/machinery/commit/a699d6af0fdfcaa198ed2b95d4017912832a289b)), closes [#12](https://github.com/stuttgart-things/machinery/issues/12) [#14](https://github.com/stuttgart-things/machinery/issues/14) [#15](https://github.com/stuttgart-things/machinery/issues/15) [#11](https://github.com/stuttgart-things/machinery/issues/11)
* update Go to 1.26.0 and k8s deps to v0.35.2 ([b1816ae](https://github.com/stuttgart-things/machinery/commit/b1816aeee478b047e6d4c44e4c5d67228369e1c9)), closes [#17](https://github.com/stuttgart-things/machinery/issues/17)
