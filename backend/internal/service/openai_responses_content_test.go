package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIResponsesMessageContent(t *testing.T) {
	for _, part := range []string{
		`{"type":"input_text","text":"hello","opaque":9007199254740993}`,
		`{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"high"}`,
		`{"type":"input_file","file_id":"file_example"}`,
		`{"type":"output_text","text":"answer","annotations":[]}`,
		`{"type":"refusal","refusal":"cannot comply"}`,
	} {
		for _, role := range []string{"user", "assistant", "developer", "system"} {
			t.Run(role+"/"+gjson.Get(part, "type").String(), func(t *testing.T) {
				body := []byte(fmt.Sprintf(` {"model":"keep","opaque":9007199254740993,"input":[{"role":%q,"content":%s},{"type":"message","role":%q,"content":%s}],"tail":"unchanged"} `, role, part, role, part))
				got, changed, err := normalizeOpenAIResponsesMessageContent(body)
				require.NoError(t, err)
				require.True(t, changed)
				for _, path := range []string{"input.0.content", "input.1.content"} {
					require.True(t, gjson.GetBytes(got, path).IsArray())
					require.Len(t, gjson.GetBytes(got, path).Array(), 1)
					require.Equal(t, part, gjson.GetBytes(got, path+".0").Raw)
				}
				require.Equal(t, "9007199254740993", gjson.GetBytes(got, "opaque").Raw)
				require.Equal(t, "unchanged", gjson.GetBytes(got, "tail").String())
				again, changed, err := normalizeOpenAIResponsesMessageContent(got)
				require.NoError(t, err)
				require.False(t, changed)
				require.Equal(t, got, again)
			})
		}
	}
}

func TestNormalizeOpenAIResponsesMessageContentPreservesOtherShapes(t *testing.T) {
	for _, body := range []string{
		`{"input":"hello"}`,
		`{"input":[{"role":"user","content":"hello"}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`,
		`{"input":[{"role":"user","content":null}]}`,
		`{"input":[{"role":"user","content":42}]}`,
		`{"input":[{"role":"user","content":{"text":"hello"}}]}`,
		`{"input":[{"role":"user","content":{"type":"future_part","data":"keep"}}]}`,
		`{"input":[{"type":"function_call_output","role":"user","content":{"type":"input_text","text":"keep"},"output":{"keep":true}}]}`,
		`{"input":[{"content":{"type":"input_text","text":"keep"}}]}`,
		`{"input":[{"role":"tool","content":{"type":"input_text","text":"keep"}}]}`,
		`{"input":[null,42,"hello"]}`,
		`{"input":[{"role":"user","content":{"type":"input_text","text":"keep"},"content":"authoritative"}]}`,
		`{"input":[{"role":"user","content":{"type":"input_text","type":"future","text":"keep"}}]}`,
		`{"input":[{"role":"user","content":{"type":"input_text","text":"keep"}}],"input":"authoritative"}`,
		`{"input":[{"role":"user","content":{"type":"input_text","text":"keep"}}]} trailing`,
	} {
		t.Run(body, func(t *testing.T) {
			got, changed, err := normalizeOpenAIResponsesMessageContent([]byte(body))
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, body, string(got))
		})
	}
}
