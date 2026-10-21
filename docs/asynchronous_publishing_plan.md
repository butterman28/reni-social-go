# Asynchronous Publishing and Instant Posting Response Plan

This document outlines the architecture and changes to decouple the "Publish Now" endpoint from synchronous third-party API execution, shifting it to a background queue to ensure immediate client responses and robust crash recovery.

## Current Behavior
When a user clicks "Publish Now" on a post, the request blocks synchronously while the API server (`cmd/api`) sequentially hits the third-party platforms (X, LinkedIn, Meta). This can cause request timeouts, slow user experiences, and thread blocking under load.

## Proposed Behavior
Clicking "Publish Now" will immediately transition the post status to `publishing`, generate an attempt ID, push the post ID onto the Redis publishing queue, and return the attempt ID back to the client immediately. The supervised background worker (`cmd/worker`) will pick up the post from Redis and publish it asynchronously.

---

## The Worker / Consumer Flow
The background worker is fully equipped to handle this asynchronous execution.
In `cmd/worker/main.go`, the worker runs the supervised `StartConsumer` loop. This loop:
1. Blocks and listens on the Redis queue defined by `config.C.ScheduledPostsQueue` (using `BRPOP`).
2. Picks up any enqueued `postID`.
3. Invokes `publishPost`, which fetches the post and its connections from the database.
4. Fans out the publishing calls to the platform adapters in parallel goroutines.
5. Saves results and finalizes the post status.

By pushing the `postID` from `PublishNow` into the same `config.C.ScheduledPostsQueue`, we reuse the existing consumer worker pipeline, ensuring that both instant and scheduled posts are published using the exact same robust execution engine.

---

## Worker Crash Recovery (Zombie Post Handling)
**The Bug:** If the `cmd/worker` container crashes, restarts, or gets OOM-killed while `publishPost` is executing, the post's status will remain `publishing` in the database forever. The `StartPoller` only looks for `status='scheduled'`, meaning there is no mechanism to recover posts stuck in `publishing`. The user will see an infinite spinner on the frontend.

**The Solution:** We teach the `StartPoller` to recognize and recover these "zombie" posts by checking for posts stuck in `publishing` for longer than a safe timeout threshold. Because the existing `publishPost` logic checks `publish_results` for already-successful platforms, the recovered run will safely skip platforms that succeeded before the crash and only retry the failed/pending ones.

### 1. Update `StartPoller` (`internal/scheduler/engine.go`)
Modify the SQL query in `pollAndEnqueue` to also select posts stuck in `publishing`. When recovered, we generate a new `current_publish_attempt_id` and increment `retry_count` to prevent infinite crash loops.

```go
func pollAndEnqueue(db *sql.DB) error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    zombieTimeoutMins := config.C.ZombiePostTimeoutMinutes
    if zombieTimeoutMins <= 0 {
        zombieTimeoutMins = 15 // Fallback
    }

    // UPDATED QUERY: Now includes zombie detection
    query := `
    WITH due AS (
        SELECT id, status FROM posts
        WHERE (
            -- 1. Normal scheduled posts
            (status = 'scheduled' AND scheduled_at <= NOW())
            OR 
            -- 2. Zombie recovery: posts stuck in 'publishing' for too long
            (status = 'publishing' AND updated_at < NOW() - ($1 || ' minutes')::INTERVAL)
        )
        FOR UPDATE SKIP LOCKED
    )
    UPDATE posts
    SET status = 'publishing',
        updated_at = NOW(),
        current_publish_attempt_id = gen_random_uuid(),
        -- Increment retry_count ONLY for zombies to prevent infinite crash loops
        retry_count = CASE 
            WHEN due.status = 'publishing' THEN posts.retry_count + 1 
            ELSE posts.retry_count 
        END
    FROM due
    WHERE posts.id = due.id
    RETURNING posts.id::text
    `

    rows, err := db.QueryContext(ctx, query, zombieTimeoutMins)
    // ... (rest of existing enqueue logic remains the same)
}

ok do we walk 