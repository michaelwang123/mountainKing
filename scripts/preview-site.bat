@echo off
REM ============================================
REM  MountainKing GitHub Pages - 本地预览脚本
REM  用法: 双击运行此脚本，然后浏览器访问 http://localhost:3000
REM ============================================

echo.
echo  ⛰️  MountainKing Documentation Site - Local Preview
echo  ====================================================
echo.

REM 进入项目根目录
cd /d "%~dp0.."

REM 创建临时预览目录
if exist "_preview" rmdir /s /q "_preview"
mkdir "_preview"

REM 复制 docs 静态文件
xcopy /E /I /Y "docs" "_preview" >nul

REM 复制 official_document markdown 文件
mkdir "_preview\official_document"
xcopy /Y "official_document\*.md" "_preview\official_document\" >nul

echo  ✅ Site files prepared in _preview/
echo.
echo  🌐 Starting local server at: http://localhost:3000
echo  📖 Documentation page:       http://localhost:3000/doc.html
echo.
echo  Press Ctrl+C to stop the server.
echo.

REM 尝试用 Python 启动 HTTP 服务器
where python >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    python -m http.server 3000 --directory _preview
    goto :cleanup
)

REM 尝试用 python3
where python3 >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    python3 -m http.server 3000 --directory _preview
    goto :cleanup
)

REM 尝试用 npx serve
where npx >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    npx serve _preview -p 3000
    goto :cleanup
)

echo  ❌ 未找到可用的 HTTP 服务器工具。
echo     请安装以下任意一个：
echo       - Python: https://www.python.org/downloads/
echo       - Node.js (npx serve): https://nodejs.org/
echo.
pause

:cleanup
REM 清理临时目录
if exist "_preview" rmdir /s /q "_preview"
