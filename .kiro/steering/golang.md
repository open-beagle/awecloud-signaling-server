---
inclusion: manual
---

# Go 代码规范

## Import 分组规范

Go 文件的 import 必须分为三组，每组之间用空行分隔：

1. **标准库**：Go 内置包
2. **第三方库**：外部依赖包
3. **本项目包**：当前项目的内部包

### 正确示例

```go
import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/your-org/your-project/internal/config"
	"github.com/your-org/your-project/internal/logger"
	"github.com/your-org/your-project/internal/model"
)
```

### 错误示例

```go
// 错误：第三方库和本项目包混在一起
import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/your-org/your-project/internal/config"  // 应该在下一组
	"github.com/your-org/your-project/internal/model"
)
```

### 分组判断规则

| 分组     | 判断条件                 | 示例                                   |
| -------- | ------------------------ | -------------------------------------- |
| 标准库   | 不含 `.` 的包路径        | `fmt`、`net/http`、`context`           |
| 第三方库 | 含 `.` 但不是本项目      | `github.com/gin-gonic/gin`             |
| 本项目包 | 本项目的 module 路径开头 | `github.com/your-org/your-project/...` |

### 特殊情况

- **别名导入**：保持在对应分组内

```go
import (
	gormlogger "gorm.io/gorm/logger"  // 第三方库组

	pb "github.com/your-org/your-project/pkg/proto"  // 本项目包组
)
```

- **匿名导入**：保持在对应分组内

```go
import (
	_ "github.com/mattn/go-sqlite3"  // 第三方库组
)
```

- **单个 import**：无需分组

```go
import "fmt"
```

### 工具检查

使用 `goimports` 自动格式化：

```bash
goimports -w .
```
