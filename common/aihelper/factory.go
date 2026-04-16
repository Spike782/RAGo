package aihelper

import (
	"context"
	"fmt"
	"sync"
)

type ModelCreator func(ctx context.Context, config map[string]interface{}) (AIModel, error)

type AIModelFactory struct {
	creators map[string]ModelCreator
}

var (
	globalFactory *AIModelFactory
	factoryOnce   sync.Once
)

func GetGlobalFactory() *AIModelFactory {
	factoryOnce.Do(func() {
		globalFactory = &AIModelFactory{
			creators: make(map[string]ModelCreator),
		}
		globalFactory.registerCreators()
	})
	return globalFactory
}

func (f *AIModelFactory) registerCreators() {
	f.creators["1"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		return NewOpenAIModel(ctx)
	}

	f.creators["2"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		userEmail, ok := config["email"].(string)
		if !ok {
			return nil, fmt.Errorf("RAG model requires email")
		}
		return NewAliRAGModel(ctx, userEmail)
	}

	f.creators["3"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		userEmail, ok := config["email"].(string)
		if !ok {
			return nil, fmt.Errorf("MCP model requires email")
		}
		return NewMCPModel(ctx, userEmail)
	}
}

func (f *AIModelFactory) CreateAIModel(ctx context.Context, modelType string, config map[string]interface{}) (AIModel, error) {
	creator, ok := f.creators[modelType]
	if !ok {
		return nil, fmt.Errorf("unsupported model type: %s", modelType)
	}
	return creator(ctx, config)
}

func (f *AIModelFactory) CreateAIHelper(ctx context.Context, modelType string, SessionID string, config map[string]interface{}) (*AIHelper, error) {
	model, err := f.CreateAIModel(ctx, modelType, config)
	if err != nil {
		return nil, err
	}
	return NewAIHelper(model, SessionID), nil
}

func (f *AIModelFactory) RegisterModel(modelType string, creator ModelCreator) {
	f.creators[modelType] = creator
}
