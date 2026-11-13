# 0.3.0-rc.1 离线试用升级包

包用于宿主 Agent 自助安装，含五平台程序包、源码、校验和与确切提交。没有 GitHub Release，不自动合并主分支，也不自动升级用户库。

必须先读 AGENT_INSTALL.md。默认 setup --mode assist，同步更新 service 与 owned hooks，移除旧 gate/Stop 整理；模式持续适用于全部已绑定项目，无逐轮选择。保持原 data_dir，停进程完整备份后再打开库。不修改 AGENTS，不替用户清洗，不补证据 link，不标完成历史 pending。

程序校验与模拟协议回归是自动验证；真实宿主 hook 到达、检索判断、规范遵循按 HOST_TRIAL.md 另行实测。交付时列出二者，不互相冒充。
