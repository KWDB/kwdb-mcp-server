#!/bin/bash

# 从version.go文件中获取版本号
VERSION=$(grep -o '"v[^"]*"' pkg/version/version.go | tr -d '"')

# 创建临时目录
mkdir -p temp

# 定义目标平台
PLATFORMS=(
    "windows:amd64:.exe"
    "darwin:amd64:"
    "linux:amd64:"
)

# 清理旧的构建文件
rm -rf bin/*.zip
rm -rf bin/*.tar.gz

# 遍历平台进行编译和打包
for platform in "${PLATFORMS[@]}"; do
    # 分割平台信息
    IFS=":" read -r os arch ext <<< "${platform}"
    
    echo "Building for $os/$arch..."
    
    # 设置输出目录
    output_dir="temp/kwdb-mcp-server-${VERSION}-${os}-${arch}"
    mkdir -p "${output_dir}"
    
    # 编译
    GOOS=${os} GOARCH=${arch} go build -o "${output_dir}/kwdb-mcp-server${ext}" cmd/kwdb-mcp-server/main.go
    
    # 复制必要文件
    cp README.md "${output_dir}/"
    cp README_zh.md "${output_dir}/"
    cp -r docs "${output_dir}/"
    
    # 创建打包文件
    if [ "$os" = "windows" ]; then
        # Windows使用zip
        cd temp
        zip -r "../bin/kwdb-mcp-server-${VERSION}-${os}-${arch}.zip" "kwdb-mcp-server-${VERSION}-${os}-${arch}"
        cd ..
    else
        # Linux和macOS使用tar.gz
        cd temp
        tar -czf "../bin/kwdb-mcp-server-${VERSION}-${os}-${arch}.tar.gz" "kwdb-mcp-server-${VERSION}-${os}-${arch}"
        cd ..
    fi
    
    echo "Package created for $os/$arch"
done

# 清理临时文件
rm -rf temp

echo "Build complete! Packages are available in the bin directory:"
ls -l bin/ 