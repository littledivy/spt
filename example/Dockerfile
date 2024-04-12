FROM debian:bookworm-slim

RUN apt-get update -y --fix-missing
RUN apt-get install -y curl make g++ autoconf libtool unzip xz-utils tar wget git

RUN /bin/bash -o pipefail -c "$(curl -fsSL https://raw.githubusercontent.com/littledivy/spt/main/install.sh)"

CMD ["/bin/bash"]
