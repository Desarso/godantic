package models

type Model_Response struct {
	Parts []Model_Part `json:"parts"`
}

//may be a string or a function call and it will be parts

type FunctionCall struct {
	ID   string                 `json:"id,omitempty"` // Unique ID for this specific call instance
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type Model_Part struct {
	Text         *string       `json:"text,omitempty"`
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`
}

type Model_Text_Part struct {
	Text string `json:"text"`
}

type Model_Text_Part_Delta struct {
	Text string `json:"text"`
}

type Model_Function_Call_Part struct {
	FunctionCall FunctionCall `json:"functionCall"`
}
