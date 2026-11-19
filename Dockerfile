# etoki のコンテナイメージ（ADR 0099）。
#
# **ここではビルドしない。** 中身は `make dist` が devShell で作った linux の
# 配布物で、それを詰めるだけにしてある。ツールチェーンを flake.nix に限るため
# （ADR 0002）。
#
# **RUN を置かない。** 置くと、ほかの CPU 向けのイメージを組むときに、その CPU の
# 真似（QEMU）が要る。利用者と DB の置き場所は、数字の UID と、持ち主ごと写す
# 空のディレクトリで用意する。
#
# **土台は alpine。** シェルがあるので、SQLite の複製の道具（Litestream など）を
# 上に重ねたとき、起動の手順（戻す → 移行 → 起動）をシェルで書ける（#472）。
FROM alpine:3.22

# BuildKit が組む先に合わせて渡す（docker build の既定も同じ）。
ARG TARGETOS
ARG TARGETARCH

COPY dist/${TARGETOS}_${TARGETARCH}/etoki/ /opt/etoki/
COPY --chown=10001:10001 dist/data/ /var/lib/etoki/

# どれも環境変数で上書きできる。コンテナの中ではループバックの外で待ち受けないと
# 外から届かない。手元で使うなら、ホストの側で 127.0.0.1 にだけ出す
# （README の「配布物から動かす」）。
ENV PATH=/opt/etoki:$PATH \
    ETOKI_ADDR=0.0.0.0:8080 \
    ETOKI_DB_PATH=/var/lib/etoki/etoki.db \
    ETOKI_WEB_DIR=/opt/etoki/web

USER 10001:10001
WORKDIR /var/lib/etoki
EXPOSE 8080

# マイグレーションは起動のたびに当てない（ADR 0099）。先に `migrate` を回す。
ENTRYPOINT ["etoki"]
