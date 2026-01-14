---
name: docs
description: A custom output style for the documentation in this project.
---

# Custom Style Instructions

You are an interactive CLI tool that helps users with software engineering tasks. You should use these behaviors when modifying any markdown files in the `docs` directory.

## Specific Behaviors

- Use kebab case for documentation file names, e.g. `my-doc.md`.
- Write in an active voice.
- Use full and grammatically correct sentences that are preferably between 7 and 25 words long.
- Use a semi-colon to link similar ideas or manage sentences that are getting over-long.
- Use articles (a, an, and the) when appropriate.
- Do not refer to an object as 'it' unless the object 'it' refers to is in the same sentence.
- Each file should have one first level heading, and multiple second level headings.  Third and fourth level headings should be used for prominent content only.
- Headings should follow title capitalization rules.
- Acronyms should include a definition alongside their first usage.
- Use the term `Postgres` instead of `PostgreSQL`.
- Use the following terminology definitions consistently:
  - Host: The underlying compute resource that runs database instances; each Control Plane server is identified by a host ID.
  - Cluster: A collection of hosts joined together to provide a unified API for managing databases.
  - Database: A Postgres database optionally replicated between multiple Postgres instances; composed of one or more nodes.
  - Node: A Spock node that uses logical replication to distribute changes to other nodes in the database.
  - Instance: A Postgres instance; each node has one or more instances (one primary, others are read replicas).
  - Orchestrator: A system that manages deployment of database instances (currently Docker Swarm).
- Include a qualifier if you must use a term outside of its documented definition, e.g. "Docker Swarm node" for the term "node".
- Each heading should have an introductory sentence or paragraph that explains the feature shown/discussed in the following section.
- Always leave a blank line before the first item in any list or sub-list (a sub-list may be code or indented bullets under a bullet item).
- Each entry in a bulleted list should be a complete sentence with articles.
- Do not use bold font bullet items.
- Do not use a numbered list unless the steps in the list need to be performed in order.
- If a section contains code or a code snippet, there should be an explanatory sentence before the code that ends with a colon, for example: "In the following example, the command_name command uses a column named my_column to accomplish description-of-what-the-code-does:"
- Multi-line code blocks should include the Linguist-compatible language tag after the opening fence, e.g:

```sql
SELECT * FROM code;
SELECT * FROM code;
SELECT * FROM code;
```

- Tabbed examples, such as demonstrating an API request with both `curl` and `restish`, should use the following style:

=== "curl"

    `curl`-specific text, if any.

    ```sh
    curl http://localhost:3000/v1/databases/example
    ```

=== "restish"

    `restish`-specific text, if any.

    ```sh
    restish get-database example
    ```

- Callouts/admonitions that do not require a title should use the GitHub style:

> [!NOTE]
> This is an admonition.

- Callouts/admonitions that do require a title should use the following style:

!!! warning "This is a title"

    This is the body of the admonition
