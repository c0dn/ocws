import { tool } from "@opencode-ai/plugin";

export default tool({
  description: "Live-test echo tool.",
  args: {
    text: tool.schema.string().describe("Text to echo"),
    repeat: tool.schema.number().int().default(1).describe("Times to repeat"),
  },
  async execute(args, context) {
    return `echo: ${Array(args.repeat).fill(args.text).join(" ")} (${context.directory})`;
  },
});
