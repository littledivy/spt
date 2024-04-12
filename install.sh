#!/bin/bash

GO_VERSION=1.22.2
GO_URL=https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz
INSTALL_DIR=/usr/local

dependencies=(wget tar git)
for dep in "${dependencies[@]}"; do
    if ! command -v $dep &> /dev/null; then
        echo "$dep is required to install spt"
        exit 1
    fi
done

if ! command -v go &> /dev/null; then
  wget -c $GO_URL -O go.tar.gz && \
      tar -C $INSTALL_DIR -xzf go.tar.gz && \
      rm go.tar.gz
  export PATH=$PATH:$INSTALL_DIR/go/bin
fi

git clone https://github.com/littledivy/spt
cd spt && make install
cd .. && rm -rf spt

echo "Installed spt successfully!"

if [[ ":$PATH:" != *":/usr/local/bin:"* ]]; then
    echo "Add /usr/local/bin to your PATH"
fi
