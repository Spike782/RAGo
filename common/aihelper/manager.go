package aihelper

import (
	"context"
	"sync"
)

var ctx = context.Background()

type AIHelperManager struct {
	helpers map[string]map[string]*AIHelper
	mu      sync.RWMutex
}

func NewAIHelperManager() *AIHelperManager {
	return &AIHelperManager{
		helpers: make(map[string]map[string]*AIHelper),
	}
}

// Get or create session-level AIHelper; preserve messages when switching model.
func (m *AIHelperManager) GetOrCreateAIHelper(userEmail string, sessionID string, modelType string, config map[string]interface{}) (*AIHelper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[userEmail]
	if !exists {
		userHelpers = make(map[string]*AIHelper)
		m.helpers[userEmail] = userHelpers
	}

	helper, exists := userHelpers[sessionID]
	if exists {
		if helper.GetModelType() == modelType {
			return helper, nil
		}

		factory := GetGlobalFactory()
		newHelper, err := factory.CreateAIHelper(ctx, modelType, sessionID, config)
		if err != nil {
			return nil, err
		}

		// Keep session history when switching model in the same session.
		newHelper.messages = helper.GetMessages()
		if closable, ok := helper.model.(interface{ Close() }); ok {
			closable.Close()
		}
		userHelpers[sessionID] = newHelper
		return newHelper, nil
	}

	factory := GetGlobalFactory()
	helper, err := factory.CreateAIHelper(ctx, modelType, sessionID, config)
	if err != nil {
		return nil, err
	}

	userHelpers[sessionID] = helper
	return helper, err
}

func (m *AIHelperManager) GetAIHelper(userEmail string, sessionID string) (*AIHelper, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userEmail]
	if !exists {
		return nil, false
	}

	helper, exists := userHelpers[sessionID]
	return helper, exists
}

func (m *AIHelperManager) RemoveAIHelper(userEmail string, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[userEmail]
	if !exists {
		return
	}
	if helper, ok := userHelpers[sessionID]; ok {
		if closable, ok := helper.model.(interface{ Close() }); ok {
			closable.Close()
		}
	}
	delete(userHelpers, sessionID)

	if len(userHelpers) == 0 {
		delete(m.helpers, userEmail)
	}
}
func (m *AIHelperManager) GetUserSession(userEmail string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userEmail]
	if !exists {
		return []string{}
	}

	sessionIDs := make([]string, 0, len(userHelpers))
	for sessionID := range userHelpers {
		sessionIDs = append(sessionIDs, sessionID)
	}
	return sessionIDs
}

var globalManager *AIHelperManager
var once sync.Once

// Get global singleton manager.
func GetGlobalManager() *AIHelperManager {
	once.Do(func() {
		globalManager = NewAIHelperManager()
	})

	return globalManager
}
