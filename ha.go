package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type HAClient struct {
	conn *websocket.Conn
	next atomic.Int64
	mu   sync.Mutex
}

type LightLocation struct {
	Floor string
	Area  string
}

func (c *HAClient) LightLocations(ctx context.Context) (map[string]LightLocation, error) {
	entities, err := c.Call(ctx, "config/entity_registry/list", nil)
	if err != nil {
		return nil, err
	}
	areas, err := c.Call(ctx, "config/area_registry/list", nil)
	if err != nil {
		return nil, err
	}
	floors, err := c.Call(ctx, "config/floor_registry/list", nil)
	if err != nil {
		return nil, err
	}
	devices, err := c.Call(ctx, "config/device_registry/list", nil)
	if err != nil {
		return nil, err
	}
	areaNames := map[string]string{}
	areaFloors := map[string]string{}
	for _, raw := range resultItems(areas) {
		var value struct {
			AreaID  string `json:"area_id"`
			Name    string `json:"name"`
			FloorID string `json:"floor_id"`
		}
		if json.Unmarshal(raw, &value) == nil {
			areaNames[value.AreaID] = value.Name
			areaFloors[value.AreaID] = value.FloorID
		}
	}
	floorNames := map[string]string{}
	for _, raw := range resultItems(floors) {
		var value struct {
			FloorID string `json:"floor_id"`
			Name    string `json:"name"`
		}
		if json.Unmarshal(raw, &value) == nil {
			floorNames[value.FloorID] = value.Name
		}
	}
	deviceAreas := map[string]string{}
	for _, raw := range resultItems(devices) {
		var value struct {
			ID     string `json:"id"`
			AreaID string `json:"area_id"`
		}
		if json.Unmarshal(raw, &value) == nil {
			deviceAreas[value.ID] = value.AreaID
		}
	}
	locations := map[string]LightLocation{}
	for _, raw := range resultItems(entities) {
		var value struct {
			EntityID string `json:"entity_id"`
			AreaID   string `json:"area_id"`
			DeviceID string `json:"device_id"`
		}
		if json.Unmarshal(raw, &value) == nil && strings.HasPrefix(value.EntityID, "light.") {
			if value.AreaID == "" {
				value.AreaID = deviceAreas[value.DeviceID]
			}
			locations[value.EntityID] = LightLocation{Area: areaNames[value.AreaID], Floor: floorNames[areaFloors[value.AreaID]]}
		}
	}
	return locations, nil
}

func resultItems(response map[string]any) [][]byte {
	values, _ := response["result"].([]any)
	items := make([][]byte, 0, len(values))
	for _, value := range values {
		if encoded, err := json.Marshal(value); err == nil {
			items = append(items, encoded)
		}
	}
	return items
}

func (c *HAClient) States(ctx context.Context, entities []string) (map[string]LightState, error) {
	response, err := c.Call(ctx, "get_states", nil)
	if err != nil {
		return nil, err
	}
	data, ok := response["result"].([]any)
	if !ok {
		return nil, fmt.Errorf("HA get_states returned unexpected result")
	}
	wanted := map[string]bool{}
	for _, entity := range entities {
		wanted[entity] = true
	}
	result := map[string]LightState{}
	for _, item := range data {
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var state LightState
		if err := json.Unmarshal(encoded, &state); err != nil {
			return nil, err
		}
		if len(entities) == 0 || wanted[state.EntityID] {
			result[state.EntityID] = state
		}
	}
	return result, nil
}

func (c *HAClient) CallService(ctx context.Context, domain, service, entityID string, data map[string]any) error {
	serviceData := map[string]any{}
	for key, value := range data {
		serviceData[key] = value
	}
	_, err := c.Call(ctx, "call_service", map[string]any{
		"domain":       domain,
		"service":      service,
		"target":       map[string]any{"entity_id": entityID},
		"service_data": serviceData,
	})
	return err
}

func ConnectHA(ctx context.Context, baseURL, token string) (*HAClient, error) {
	wsURL := strings.TrimRight(baseURL, "/")
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1) + "/api/websocket"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("connect HA: %w", err)
	}
	client := &HAClient{conn: conn}
	var hello struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&hello); err != nil || hello.Type != "auth_required" {
		conn.Close()
		return nil, fmt.Errorf("HA websocket auth handshake failed")
	}
	if err := conn.WriteJSON(map[string]any{"type": "auth", "access_token": token}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("authenticate HA: %w", err)
	}
	var auth struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := conn.ReadJSON(&auth); err != nil || auth.Type != "auth_ok" {
		conn.Close()
		if auth.Message == "" {
			auth.Message = "authentication rejected"
		}
		return nil, fmt.Errorf("authenticate HA: %s", auth.Message)
	}
	return client, nil
}

func (c *HAClient) Close() error { return c.conn.Close() }

func (c *HAClient) Call(ctx context.Context, command string, payload map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.next.Add(1)
	message := map[string]any{"id": id, "type": command}
	for key, value := range payload {
		message[key] = value
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
		_ = c.conn.SetReadDeadline(deadline)
	} else {
		_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		_ = c.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	}
	if err := c.conn.WriteJSON(message); err != nil {
		return nil, fmt.Errorf("HA command %s: %w", command, err)
	}
	for {
		var response map[string]any
		if err := c.conn.ReadJSON(&response); err != nil {
			return nil, fmt.Errorf("read HA command %s: %w", command, err)
		}
		responseID, ok := response["id"].(float64)
		if !ok || int64(responseID) != id {
			continue
		}
		if success, ok := response["success"].(bool); !ok || !success {
			message, _ := response["error"].(map[string]any)
			return nil, fmt.Errorf("HA command %s failed: %s", command, errorMessage(message))
		}
		return response, nil
	}
}

func errorMessage(value map[string]any) string {
	if value == nil {
		return "unknown error"
	}
	data, _ := json.Marshal(value)
	return string(data)
}
