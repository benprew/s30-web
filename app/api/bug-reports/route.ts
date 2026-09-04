const MAX_REQUEST_BYTES = 2_000_000;
const MAX_TITLE_LENGTH = 220;
const MAX_BODY_LENGTH = 62_000;

type BugReportPayload = {
  title?: unknown;
  body?: unknown;
  is_crash?: unknown;
  report?: unknown;
};

function json(data: unknown, status = 200) {
  return Response.json(data, {
    status,
    headers: {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'POST, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type',
      'Cache-Control': 'no-store',
    },
  });
}

export function OPTIONS() {
  return new Response(null, {
    status: 204,
    headers: {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'POST, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type',
      'Access-Control-Max-Age': '86400',
    },
  });
}

export async function POST(request: Request) {
  const contentLength = Number(request.headers.get('content-length') ?? 0);
  if (contentLength > MAX_REQUEST_BYTES) {
    return json({ success: false, error: 'Report is too large' }, 413);
  }

  if (
    !request.headers
      .get('content-type')
      ?.toLowerCase()
      .includes('application/json')
  ) {
    return json(
      { success: false, error: 'Content-Type must be application/json' },
      415,
    );
  }

  let payload: BugReportPayload;
  try {
    const rawBody = await request.text();
    if (new TextEncoder().encode(rawBody).byteLength > MAX_REQUEST_BYTES) {
      return json({ success: false, error: 'Report is too large' }, 413);
    }
    payload = JSON.parse(rawBody) as BugReportPayload;
  } catch {
    return json(
      { success: false, error: 'Request body is not valid JSON' },
      400,
    );
  }

  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
    return json(
      { success: false, error: 'Request body must be a JSON object' },
      400,
    );
  }

  const isCrash = payload.is_crash === true;
  const fallbackTitle = isCrash
    ? '[Crash] In-game crash report'
    : '[Bug] In-game report';
  const title =
    (typeof payload.title === 'string' ? payload.title : fallbackTitle)
      .replace(/[\r\n\t]+/g, ' ')
      .trim()
      .slice(0, MAX_TITLE_LENGTH) || fallbackTitle;
  const submittedBody =
    typeof payload.body === 'string' ? payload.body.trim() : '';

  if (!submittedBody) {
    return json({ success: false, error: 'Report body is required' }, 400);
  }

  const body =
    submittedBody.length > MAX_BODY_LENGTH
      ? `${submittedBody.slice(0, MAX_BODY_LENGTH)}\n\n_Report truncated by the bug report service._`
      : submittedBody;

  const token = process.env.GITHUB_TOKEN;
  if (!token) {
    return json(
      { success: false, error: 'Bug report service is not configured' },
      503,
    );
  }

  const owner = process.env.GITHUB_REPO_OWNER || 'benprew';
  const repo = process.env.GITHUB_REPO_NAME || 's30';

  try {
    const response = await fetch(
      `https://api.github.com/repos/${owner}/${repo}/issues`,
      {
        method: 'POST',
        headers: {
          Accept: 'application/vnd.github+json',
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
          'User-Agent': 's30-bug-report-service/1.0',
          'X-GitHub-Api-Version': '2022-11-28',
        },
        body: JSON.stringify({
          title,
          body,
          labels: isCrash
            ? ['in-app-report', 'crash']
            : ['in-app-report', 'bug'],
        }),
      },
    );

    if (!response.ok) {
      console.error('GitHub issue creation failed', response.status);
      return json({ success: false, error: 'Could not create the issue' }, 502);
    }

    const issue = (await response.json()) as {
      html_url?: string;
      number?: number;
    };
    if (!issue.html_url || typeof issue.number !== 'number') {
      return json(
        { success: false, error: 'GitHub returned an invalid response' },
        502,
      );
    }

    return json({
      success: true,
      issue_url: issue.html_url,
      issue_number: issue.number,
      message: `Created issue #${issue.number}`,
    });
  } catch (error) {
    console.error(
      'Bug report submission failed',
      error instanceof Error ? error.message : 'Unknown error',
    );
    return json({ success: false, error: 'Could not submit the report' }, 502);
  }
}
