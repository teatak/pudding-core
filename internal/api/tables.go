package api

import (
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"net/http"
)

func (s *Server) getStudioContent(c *cart.Context) error {
	id, _ := c.Param("itemID")
	item, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if item.Kind == store.StudioItemKindTable {
		table, err := s.store.GetTable(c.Request.Context(), id)
		if err != nil {
			return s.studioItemError(c, err)
		}
		c.JSON(http.StatusOK, table)
		return nil
	}
	return s.getDocument(c)
}
func (s *Server) writeTable(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var in store.TableWrite
	if err := decodeNativeContentRequest(c, &in); err != nil {
		return badRequest(c, err.Error())
	}
	in.Author = store.ContentAuthor{Kind: "user"}
	table, err := s.store.WriteTable(c.Request.Context(), id, in)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, table)
	return nil
}
