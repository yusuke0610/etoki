# Go のライセンス

`LICENSE` は、配る `etoki` のバイナリに焼かれる Go の標準ライブラリのライセンス。
`make dist` が配布物の `licenses/go/LICENSE` に写す（ADR 0099）。

- 出どころは Go のリポジトリの `go1.26.11` のタグの `LICENSE`
  （`https://raw.githubusercontent.com/golang/go/go1.26.11/LICENSE`）。手で書いて
  いない。
- ここに置くのは、devShell の Go（nixpkgs）が `LICENSE` を持たないため。
  `go-licenses` も標準ライブラリを数えない。
- 本文は長く変わっていないが、Go を上げて本文が変わったら写し直す。
