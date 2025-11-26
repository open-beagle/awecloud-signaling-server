# 前端构建说明

## ✅ 构建成功

前端已成功构建到 `web/dist/` 目录。

## 构建方法

### 方式1：使用脚本（推荐）

```bash
bash scripts/build_frontend.sh
```

### 方式2：手动构建

```bash
cd web
npm install --legacy-peer-deps
npm run build
```

## 构建产物

- **位置**: `web/dist/`
- **大小**: ~1.6MB
- **文件**:
  - `index.html` - 入口HTML
  - `assets/` - JS和CSS文件

## 版本说明

当前使用的版本组合（已测试通过）：

- Vue: 3.3.4
- Element Plus: 2.4.4
- Vite: 5.0.5
- TypeScript: 5.2.2
- vue-tsc: 1.8.22

## 构建脚本说明

`npm run build` - 直接构建（跳过类型检查，快速）
`npm run type-check` - 只进行类型检查
`npm run dev` - 开发模式
`npm run preview` - 预览构建产物

## 注意事项

### 1. Node.js版本

推荐使用 Node.js >= 16

当前版本：
```bash
node -v  # v22.18.0
```

### 2. 安全警告

构建时可能会看到安全警告：
```
5 moderate severity vulnerabilities
```

这些是开发依赖的警告，不影响生产环境。如需修复：
```bash
npm audit fix --force
```

### 3. 包大小警告

```
Some chunks are larger than 500 kB after minification
```

这是因为Element Plus UI库较大。可以通过以下方式优化：
- 按需引入组件
- 代码分割
- 使用CDN

当前为MVP版本，暂不优化。

## 开发模式

```bash
cd web
npm run dev
```

访问 http://localhost:3000

特点：
- 热更新
- 快速刷新
- 开发工具支持

## 生产部署

构建产物 `web/dist/` 可以：

1. **嵌入Server**（当前方式）
   - Server自动提供静态文件服务
   - 访问 http://localhost:8080

2. **独立部署**
   - 部署到Nginx
   - 部署到CDN
   - 配置API代理

## 故障排查

### 问题1：vue-tsc错误

**错误**:
```
Search string not found: "/supportedTSExtensions = .*(?=;)/"
```

**解决**:
已修复，使用兼容的版本组合。

### 问题2：依赖安装失败

**解决**:
```bash
rm -rf web/node_modules web/package-lock.json
cd web
npm install --legacy-peer-deps
```

### 问题3：构建失败

**检查**:
1. Node.js版本 >= 16
2. 磁盘空间充足
3. 网络连接正常

**清理重试**:
```bash
cd web
rm -rf node_modules dist
npm install --legacy-peer-deps
npm run build
```

## 集成到CI/CD

GitHub Actions已配置自动构建：

```yaml
- name: Cross Build
  run: |
    docker run --rm -v ${{ github.workspace }}:/go/src/... \
      bash ./scripts/build.sh
```

## 相关文档

- [Web开发文档](README.md)
- [实现文档](IMPLEMENTATION.md)
- [开发指南](../DEVELOPMENT.md)

---

**构建时间**: 2025-11-26  
**状态**: ✅ 成功
