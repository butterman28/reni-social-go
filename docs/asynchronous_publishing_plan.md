# Asynchronous Publishing and Instant Posting Response Plan

This document outlines the changes to decouple the "Publish Now" endpoint from synchronous third-party API execution, shifting it to a background queue to ensure immediate client responses.

## Current Behavior
When a user clicks "Publish Now" on a post, the request blocks synchronously while the API server (`cmd/api`) sequentially hits the third-party platforms (X, LinkedIn, Meta). This can cause request timeouts, slow user experiences, and thread blocking under load.

## Proposed Behavior
Clicking "Publish Now" will immediately transition the post status to `publishing`, generate an attempt ID, push the post ID onto the Redis publishing queue, and return the attempt ID back to the client immediately. The supervised background worker (`cmd/worker`) will pick up the post from Redis and publish it asynchronously.

---

## The Worker / Consumer Flow (No Changes Required)

The background worker is **already fully equipped** to handle this asynchronous execution without any code changes. 

In [cmd/worker/main.go](file:///c:/Users/USER/Documents/GitHub/reni-social/cmd/worker/main.go), the worker runs the supervised [StartConsumer](file:///c:/Users/USER/Documents/GitHub/reni-social/internal/scheduler/consumer.go#L28) loop. This loop:
1. Blocks and listens on the Redis queue defined by `config.C.ScheduledPostsQueue` (using `BRPOP`).
2. Picks up any enqueued `postID`.
3. Invokes [`publishPost`](file:///c:/Users/USER/Documents/GitHub/reni-social/internal/scheduler/consumer.go#L54), which fetches the post and its connections from the database.
4. Fans out the publishing calls to the platform adapters in parallel goroutines.
5. Saves results and finalizes the post status.

By pushing the `postID` from `PublishNow` into the same `config.C.ScheduledPostsQueue` queue, we reuse the existing consumer worker pipeline, ensuring that both instant and scheduled posts are published using the exact same robust execution engine.

---

## Proposed Changes

### Post Service

#### [internal/post/service.go](file:///c:/Users/USER/Documents/GitHub/reni-social/internal/post/service.go)

Modify `PublishNow` to perform the following:
1. Load the post and check if its status allows publishing (i.e., not already `published` or `publishing`).
2. Generate a new `attemptID`.
3. Update the post's status in the database to `publishing` and set `current_publish_attempt_id = attemptID`.
4. Push the `postID` to the Redis queue (`config.C.ScheduledPostsQueue`) using `utils.RDB.LPush`.
5. Revert the database updates (reverting status to its original state and setting `current_publish_attempt_id = NULL`) if the Redis queue push fails.
6. Return `attemptID` immediately, bypassing the synchronous fan-out loop.

```go
func PublishNow(ctx context.Context, postID, workspaceID string) (uuid.UUID, error) {
	post, err := GetPost(ctx, postID, workspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	if post.Status == domain.StatusPublished || post.Status == domain.StatusPublishing {
		return uuid.Nil, fmt.Errorf("post is already published or being published")
	}

	// 1. Generate the Idempotency Key for this specific button click
	attemptID := uuid.New()

	_, err = db.DB.ExecContext(ctx,
		`UPDATE posts SET status='publishing', current_publish_attempt_id=$1, updated_at=NOW() WHERE id=$2`,
		attemptID, postID)
	if err != nil {
		return uuid.Nil, err
	}

	// 2. Enqueue the post to the Redis queue for the worker to publish it asynchronously
	if err := utils.RDB.LPush(ctx, config.C.ScheduledPostsQueue, postID).Err(); err != nil {
		// Revert status on failure
		_, _ = db.DB.ExecContext(ctx,
			`UPDATE posts SET status=$1, current_publish_attempt_id=NULL, updated_at=NOW() WHERE id=$2`,
			post.Status, postID)
		return uuid.Nil, fmt.Errorf("enqueue post: %w", err)
	}

	return attemptID, nil
}
```

## Verification Plan

### Automated Tests
- Verify that the codebase compiles:
  ```bash
  go build ./...
  ```
- Run the workspace test suite:
  ```bash
  go test ./...
  ```
