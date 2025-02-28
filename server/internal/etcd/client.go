package etcd

// Interface check
// var _ do.Shutdownable = (*Client)(nil)

// type Client struct {
// 	*clientv3.Client
// }

// func (c *Client) Shutdown() error {
// 	if err := c.Close(); err != nil {
// 		return fmt.Errorf("error while closing client: %w", err)
// 	}
// 	return nil
// }
