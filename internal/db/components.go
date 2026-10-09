package db

import (
	"context"

	"github.com/quay/release-readiness/internal/db/sqlc"
	"github.com/quay/release-readiness/internal/model"
)

func (d *DB) CreateComponent(ctx context.Context, name, description string) (*model.Component, error) {
	id, err := d.queries().CreateComponent(ctx, dbsqlc.CreateComponentParams{
		Name:        name,
		Description: description,
	})
	if err != nil {
		return nil, err
	}
	return &model.Component{ID: id, Name: name, Description: description}, nil
}

func (d *DB) GetComponentByName(ctx context.Context, name string) (*model.Component, error) {
	row, err := d.queries().GetComponentByName(ctx, name)
	if err != nil {
		return nil, err
	}
	c := toComponent(row)
	return &c, nil
}

func (d *DB) EnsureComponent(ctx context.Context, name string) (*model.Component, error) {
	comp, err := d.GetComponentByName(ctx, name)
	if err == nil {
		return comp, nil
	}
	return d.CreateComponent(ctx, name, "")
}

func toComponent(r dbsqlc.Component) model.Component {
	return model.Component{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		CreatedAt:   parseTime(r.CreatedAt),
	}
}
