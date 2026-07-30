package domain

import (
	"encoding/json"
	"fmt"
)

// Excalidraw の要素タイプのうち etoki が解釈するもの。
const (
	elementTypeFrame = "frame"
	elementTypeText  = "text"
)

// Scene は Excalidraw のシーン JSON のうち etoki が必要とする部分。
//
// Excalidraw の全スキーマは追わない。注釈の帰属とテキストの抽出に要る
// フィールドだけを読み、残りは無視して素通しする。
type Scene struct {
	Elements []Element `json:"elements"`
}

// Element は Excalidraw の要素。
type Element struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	IsDeleted bool   `json:"isDeleted"`

	// FrameID は所属する frame の ID。frame に入っていなければ nil。
	// Excalidraw 側が帰属を管理して書き込むフィールドであり、etoki は
	// 座標から帰属を推測しない。
	FrameID *string `json:"frameId"`

	// ContainerID はラベルとして紐づく図形の ID。
	// 図形の中に書かれたテキストは frame の直接の子にはならず、
	// コンテナ側に属する。抽出ではこちらも辿る必要がある。
	ContainerID *string `json:"containerId"`

	// Name は frame 要素のラベル。
	Name string `json:"name"`

	// Text はテキスト要素の本文。
	Text string `json:"text"`

	// CustomData は etoki が注釈のメタデータを載せる場所。
	CustomData *CustomData `json:"customData"`
}

// CustomData は Excalidraw 要素の customData のうち etoki が使う部分。
type CustomData struct {
	Etoki *AnnotationMeta `json:"etoki"`
}

// AnnotationMeta は注釈に付与するメタデータ。
type AnnotationMeta struct {
	Granularity Granularity `json:"granularity"`
}

// Annotation は issue 化の対象として囲まれた範囲。
type Annotation struct {
	// ID は frame 要素の ID。マッピングの annotation_element_id になる。
	ID string
	// Name は frame のラベル。空でもよい。
	Name string
	// Granularity は開発者が指定した粒度。
	Granularity Granularity
}

// ParseScene はシーン JSON を読む。
func ParseScene(raw []byte) (Scene, error) {
	var s Scene
	if err := json.Unmarshal(raw, &s); err != nil {
		return Scene{}, fmt.Errorf("parse scene: %w", err)
	}
	return s, nil
}

// isAnnotation は要素が etoki の注釈かどうかを返す。
//
// frame 要素であることに加えて customData.etoki の存在を条件にしているのは、
// ブレスト中にユーザーが自分の用途で frame を使っても注釈と誤認しないため。
func (e Element) isAnnotation() bool {
	return e.Type == elementTypeFrame &&
		!e.IsDeleted &&
		e.CustomData != nil &&
		e.CustomData.Etoki != nil
}

// Annotations はシーンに含まれる注釈を要素の並び順で返す。
func (s Scene) Annotations() []Annotation {
	var annotations []Annotation
	for _, e := range s.Elements {
		if !e.isAnnotation() {
			continue
		}
		annotations = append(annotations, Annotation{
			ID:          e.ID,
			Name:        e.Name,
			Granularity: e.CustomData.Etoki.Granularity,
		})
	}
	return annotations
}

// AnnotationTexts は注釈に属するテキスト要素を返す。
//
// 集める経路は 2 つある。
//
//   - frame の直接の子（frameId が一致するテキスト要素）
//   - frame の子である図形に紐づくラベル（containerId が子の ID と一致）
//
// 後者を辿らないと、図形の中に書かれた文字がまるごとハッシュから抜ける。
// frame の名前も含める。LLM への入力になる以上、変えたら「変更あり」に
// なるべきであるため。
func (s Scene) AnnotationTexts(annotationID string) []TextElement {
	childIDs := make(map[string]struct{})
	var frameName string

	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		if e.ID == annotationID && e.Type == elementTypeFrame {
			frameName = e.Name
		}
		if e.FrameID != nil && *e.FrameID == annotationID {
			childIDs[e.ID] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	var texts []TextElement

	if frameName != "" {
		seen[annotationID] = struct{}{}
		texts = append(texts, TextElement{ID: annotationID, Text: frameName})
	}

	for _, e := range s.Elements {
		if e.IsDeleted || e.Type != elementTypeText {
			continue
		}
		if _, dup := seen[e.ID]; dup {
			continue
		}

		_, isChild := childIDs[e.ID]
		isBoundToChild := false
		if e.ContainerID != nil {
			_, isBoundToChild = childIDs[*e.ContainerID]
		}

		if !isChild && !isBoundToChild {
			continue
		}

		seen[e.ID] = struct{}{}
		texts = append(texts, TextElement{ID: e.ID, Text: e.Text})
	}

	return texts
}

// AnnotationHash は注釈のハッシュを算出する。
func (s Scene) AnnotationHash(a Annotation) ContentHash {
	return ComputeContentHash(s.AnnotationTexts(a.ID), a.Granularity)
}
