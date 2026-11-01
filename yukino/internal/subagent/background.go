package subagent

import (
	"fmt"

	"github.com/yukino-js/yukino/yukino/internal/agent"
)

func configureBackgroundRuntime(child *agent.Agent) *TaskManager {
	manager := NewTaskManager()
	child.TaskManager = manager
	child.NotificationFn = func() []string {
		var notifications []string
		for _, task := range manager.DrainNotifications() {
			notifications = append(notifications, fmt.Sprintf("<task-notification>\n<task_id>%s</task_id>\n<status>%s</status>\n<result>%s</result>\n</task-notification>", task.ID, task.Status, task.Output))
		}
		return notifications
	}
	return manager
}
