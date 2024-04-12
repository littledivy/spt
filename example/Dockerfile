FROM debian:bookworm-slim

ARG BUILD_ARG_1
ENV BUILD_ARG_1=$BUILD_ARG_1

RUN apt-get update -y --fix-missing
RUN apt-get install -y curl make g++ autoconf libtool unzip xz-utils tar wget git

RUN /bin/bash -o pipefail -c "$(curl -fsSL https://raw.githubusercontent.com/littledivy/spt/main/install.sh)"

CMD ["/bin/bash"]
