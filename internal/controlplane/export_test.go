package controlplane

// SetBackoff replaces the client's retry spacing, so a test of the retry
// budget does not wait on the production backoff.
func SetBackoff(c *Client, backoff Backoff) {
	c.backoff = backoff
}
