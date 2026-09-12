package service

// RewriteMappedRequestModel rewrites the top-level model in JSON or multipart
// requests, retaining binary multipart fields and returning the new boundary.
func RewriteMappedRequestModel(body []byte, contentType, model string) ([]byte, string, error) {
	return rewriteOpenAIImagesModel(body, contentType, model)
}
